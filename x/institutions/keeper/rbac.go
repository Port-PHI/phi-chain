// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"crypto/sha256"
	"encoding/binary"

	"cosmossdk.io/errors"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

// This file implements role-based access control (RBAC), aggregated multisig
// approval for sensitive actions, daily caps, and deposit/redeem idempotency.

// --- Role storage (RBAC) ---

// SetRole grants a role to an address within an institution.
func (k Keeper) SetRole(ctx sdk.Context, instID string, addr sdk.AccAddress, role types.InstitutionRole) {
	rg := types.RoleGrant{Institution: instID, Address: addr.String(), Role: role}
	ctx.KVStore(k.storeKey).Set(types.RoleKey(instID, addr), k.cdc.MustMarshal(&rg))
}

// GetRole returns the address's role within the institution; no grant = UNSPECIFIED.
func (k Keeper) GetRole(ctx sdk.Context, instID string, addr sdk.AccAddress) types.InstitutionRole {
	bz := ctx.KVStore(k.storeKey).Get(types.RoleKey(instID, addr))
	if bz == nil {
		return types.INSTITUTION_ROLE_UNSPECIFIED
	}
	var rg types.RoleGrant
	k.cdc.MustUnmarshal(bz, &rg)
	return rg.Role
}

// DeleteRole removes the address's role grant.
func (k Keeper) DeleteRole(ctx sdk.Context, instID string, addr sdk.AccAddress) {
	ctx.KVStore(k.storeKey).Delete(types.RoleKey(instID, addr))
}

// IterateRolesFor iterates over the roles of one institution.
func (k Keeper) IterateRolesFor(ctx sdk.Context, instID string, cb func(types.RoleGrant) bool) {
	store := ctx.KVStore(k.storeKey)
	it := storetypes.KVStorePrefixIterator(store, types.RolePrefixFor(instID))
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var rg types.RoleGrant
		k.cdc.MustUnmarshal(it.Value(), &rg)
		if cb(rg) {
			break
		}
	}
}

// IterateAllRoles iterates over all role grants of all institutions (for genesis).
func (k Keeper) IterateAllRoles(ctx sdk.Context, cb func(types.RoleGrant) bool) {
	store := ctx.KVStore(k.storeKey)
	it := storetypes.KVStorePrefixIterator(store, types.RolePrefix)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var rg types.RoleGrant
		k.cdc.MustUnmarshal(it.Value(), &rg)
		if cb(rg) {
			break
		}
	}
}

// --- Role gate ---

// effectiveRole returns the signer's effective role within the institution. The registered admin
// (inst.Admin) always has the ADMIN role (implicit root) - even without an explicit grant.
func (k Keeper) effectiveRole(ctx sdk.Context, inst types.Institution, signer sdk.AccAddress) types.InstitutionRole {
	if signer.String() == inst.Admin {
		return types.INSTITUTION_ROLE_ADMIN
	}
	return k.GetRole(ctx, inst.Id, signer)
}

// requireRole checks that the signer holds one of the allowed roles.
func (k Keeper) requireRole(ctx sdk.Context, inst types.Institution, signerBech string, allowed ...types.InstitutionRole) error {
	signer, err := sdk.AccAddressFromBech32(signerBech)
	if err != nil {
		return err
	}
	role := k.effectiveRole(ctx, inst, signer)
	for _, a := range allowed {
		if role == a {
			return nil
		}
	}
	return errors.Wrapf(types.ErrRoleNotAuthorized, "signer role=%s", role)
}

// countAdmins counts the addresses with the ADMIN role (including the implicit root inst.Admin, without double-counting).
func (k Keeper) countAdmins(ctx sdk.Context, inst types.Institution) uint32 {
	set := map[string]bool{inst.Admin: true}
	k.IterateRolesFor(ctx, inst.Id, func(rg types.RoleGrant) bool {
		if rg.Role == types.INSTITUTION_ROLE_ADMIN {
			set[rg.Address] = true
		}
		return false
	})
	return uint32(len(set))
}

// --- Aggregated-approval multisig (content hash) ---

// effectiveThreshold returns the effective threshold for a sensitive action = min(threshold, admin count) - anti-deadlock.
func (k Keeper) effectiveThreshold(ctx sdk.Context, inst types.Institution) uint32 {
	t := inst.Params.SensitiveThreshold
	if t == 0 {
		t = types.DefaultSensitiveThreshold
	}
	admins := k.countAdmins(ctx, inst)
	if admins == 0 {
		admins = 1 // should not happen; the implicit root always exists
	}
	if t > admins {
		return admins
	}
	return t
}

// adminEpoch returns the institution's current admin-set epoch (0 if never bumped).
func (k Keeper) adminEpoch(ctx sdk.Context, instID string) uint64 {
	bz := ctx.KVStore(k.storeKey).Get(types.AdminEpochKey(instID))
	if len(bz) != 8 {
		return 0
	}
	return binary.BigEndian.Uint64(bz)
}

// bumpAdminEpoch advances the admin-set epoch, invalidating every pending multisig approval recorded
// under the previous epoch. Called whenever the institution's ADMIN set changes.
func (k Keeper) bumpAdminEpoch(ctx sdk.Context, instID string) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], k.adminEpoch(ctx, instID)+1)
	ctx.KVStore(k.storeKey).Set(types.AdminEpochKey(instID), b[:])
}

// recordApproval records an approval (stamped with the current admin epoch) and returns the count of
// still-valid distinct approvals.
func (k Keeper) recordApproval(ctx sdk.Context, inst types.Institution, contentHash []byte, signer sdk.AccAddress) uint32 {
	var epoch [8]byte
	binary.BigEndian.PutUint64(epoch[:], k.adminEpoch(ctx, inst.Id))
	ctx.KVStore(k.storeKey).Set(types.ApprovalKey(inst.Id, contentHash, signer), epoch[:])
	return k.countApprovals(ctx, inst, contentHash)
}

// countApprovals returns the count of valid distinct approvals for a sensitive action. An
// approval marker counts only if (a) it was cast under the CURRENT admin epoch — so any change to the
// admin set since it was cast invalidates it — and (b) its signer is STILL an effective ADMIN, so an
// approval from an address that has since been demoted no longer helps reach the threshold. Either
// condition alone closes the shrink-then-execute bypass; both are enforced for defense in depth.
func (k Keeper) countApprovals(ctx sdk.Context, inst types.Institution, contentHash []byte) uint32 {
	store := ctx.KVStore(k.storeKey)
	prefix := types.ApprovalPrefixFor(inst.Id, contentHash)
	epoch := k.adminEpoch(ctx, inst.Id)
	it := storetypes.KVStorePrefixIterator(store, prefix)
	defer it.Close()
	var n uint32
	for ; it.Valid(); it.Next() {
		if len(it.Value()) != 8 || binary.BigEndian.Uint64(it.Value()) != epoch {
			continue // stale: cast under a superseded admin set
		}
		signer := sdk.AccAddress(it.Key()[len(prefix):])
		if k.effectiveRole(ctx, inst, signer) != types.INSTITUTION_ROLE_ADMIN {
			continue // the approver is no longer an admin
		}
		n++
	}
	return n
}

// clearApprovals clears all approvals for a sensitive action after execution.
func (k Keeper) clearApprovals(ctx sdk.Context, instID string, contentHash []byte) {
	store := ctx.KVStore(k.storeKey)
	it := storetypes.KVStorePrefixIterator(store, types.ApprovalPrefixFor(instID, contentHash))
	var keys [][]byte
	for ; it.Valid(); it.Next() {
		keys = append(keys, append([]byte(nil), it.Key()...))
	}
	it.Close()
	for _, key := range keys {
		store.Delete(key)
	}
}

// contentHashOf is the deterministic content hash of a sensitive action (each part length-prefixed, independent of the signer).
func contentHashOf(parts ...[]byte) []byte {
	h := sha256.New()
	var lenbuf [4]byte
	for _, p := range parts {
		binary.BigEndian.PutUint32(lenbuf[:], uint32(len(p)))
		h.Write(lenbuf[:])
		h.Write(p)
	}
	return h.Sum(nil)
}

func roleBytes(r types.InstitutionRole) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(r))
	return b[:]
}

// --- Daily caps ---

// dayIndex returns the UTC day number from the block time (deterministic).
func dayIndex(ctx sdk.Context) int64 {
	return ctx.BlockTime().Unix() / 86400
}

func (k Keeper) getCounterTotal(ctx sdk.Context, instID, kind string, day int64) math.Int {
	return parseCounter(ctx.KVStore(k.storeKey).Get(types.CounterTotalKey(instID, kind, day)))
}

func (k Keeper) getCounterUser(ctx sdk.Context, instID, kind string, day int64, addr sdk.AccAddress) math.Int {
	return parseCounter(ctx.KVStore(k.storeKey).Get(types.CounterUserKey(instID, kind, day, addr)))
}

func (k Keeper) addCounterTotal(ctx sdk.Context, instID, kind string, day int64, amt math.Int) {
	v := k.getCounterTotal(ctx, instID, kind, day).Add(amt)
	ctx.KVStore(k.storeKey).Set(types.CounterTotalKey(instID, kind, day), []byte(v.String()))
}

func (k Keeper) addCounterUser(ctx sdk.Context, instID, kind string, day int64, addr sdk.AccAddress, amt math.Int) {
	v := k.getCounterUser(ctx, instID, kind, day, addr).Add(amt)
	ctx.KVStore(k.storeKey).Set(types.CounterUserKey(instID, kind, day, addr), []byte(v.String()))
}

func parseCounter(bz []byte) math.Int {
	if bz == nil {
		return math.ZeroInt()
	}
	if v, ok := math.NewIntFromString(string(bz)); ok {
		return v
	}
	return math.ZeroInt()
}

// enforceMintCaps checks the mint caps before minting (per-tx, daily, per-user) plus the recipient's
// KYC-tier daily limit.
func (k Keeper) enforceMintCaps(ctx sdk.Context, inst types.Institution, recipient sdk.AccAddress, toman math.Int, kycTier uint32) error {
	c := inst.Params.Caps
	day := dayIndex(ctx)
	// Protocol mint ceiling: a hard upper bound on every mint, enforced even when the
	// institution sets no cap of its own, so one institution cannot mint an unbounded amount.
	mp := k.GetParams(ctx)
	if ceil := types.CapInt(mp.MintCeilingPerTx); ceil.IsPositive() && toman.GT(ceil) {
		return errors.Wrapf(types.ErrCapExceeded, "mint per_tx %s exceeds protocol ceiling %s", toman, ceil)
	}
	if ceil := types.CapInt(mp.MintCeilingDaily); ceil.IsPositive() {
		if k.getCounterTotal(ctx, inst.Id, "md", day).Add(toman).GT(ceil) {
			return errors.Wrapf(types.ErrCapExceeded, "mint daily exceeds protocol ceiling %s", ceil)
		}
	}
	if lim := types.CapInt(c.MintPerTx); lim.IsPositive() && toman.GT(lim) {
		return errors.Wrapf(types.ErrCapExceeded, "mint per_tx: %s > %s", toman, lim)
	}
	if lim := types.CapInt(c.MintDaily); lim.IsPositive() {
		if k.getCounterTotal(ctx, inst.Id, "md", day).Add(toman).GT(lim) {
			return errors.Wrapf(types.ErrCapExceeded, "mint daily cap %s", lim)
		}
	}
	if lim := types.CapInt(c.MintPerUser); lim.IsPositive() {
		if k.getCounterUser(ctx, inst.Id, "mu", day, recipient).Add(toman).GT(lim) {
			return errors.Wrapf(types.ErrCapExceeded, "mint per_user cap %s", lim)
		}
	}
	if lim := kycTierDailyLimit(inst.Params, kycTier); lim.IsPositive() {
		if k.getCounterUser(ctx, inst.Id, "mu", day, recipient).Add(toman).GT(lim) {
			return errors.Wrapf(types.ErrKycTierExceeded, "mint KYC tier %d daily limit %s", kycTier, lim)
		}
	}
	return nil
}

// addMintCounters increments the mint counters after success.
func (k Keeper) addMintCounters(ctx sdk.Context, inst types.Institution, recipient sdk.AccAddress, toman math.Int) {
	day := dayIndex(ctx)
	k.addCounterTotal(ctx, inst.Id, "md", day, toman)
	k.addCounterUser(ctx, inst.Id, "mu", day, recipient, toman)
}

// enforceRedeemCaps checks the redeem caps before burning: the institution's per-tx/daily/per-user
// caps, the holder's KYC-tier daily limit, and the network-wide emergency brake.
func (k Keeper) enforceRedeemCaps(ctx sdk.Context, inst types.Institution, holder sdk.AccAddress, toman math.Int, kycTier uint32) error {
	c := inst.Params.Caps
	day := dayIndex(ctx)
	if lim := types.CapInt(c.RedeemPerTx); lim.IsPositive() && toman.GT(lim) {
		return errors.Wrapf(types.ErrCapExceeded, "redeem per_tx: %s > %s", toman, lim)
	}
	if lim := types.CapInt(c.RedeemDaily); lim.IsPositive() {
		if k.getCounterTotal(ctx, inst.Id, "rd", day).Add(toman).GT(lim) {
			return errors.Wrapf(types.ErrCapExceeded, "redeem daily cap %s", lim)
		}
	}
	if lim := types.CapInt(c.RedeemPerUser); lim.IsPositive() {
		if k.getCounterUser(ctx, inst.Id, "ru", day, holder).Add(toman).GT(lim) {
			return errors.Wrapf(types.ErrCapExceeded, "redeem per_user cap %s", lim)
		}
	}
	// KYC-tier daily limit: the per-user daily redemption must stay within the holder's tier limit.
	if lim := kycTierDailyLimit(inst.Params, kycTier); lim.IsPositive() {
		if k.getCounterUser(ctx, inst.Id, "ru", day, holder).Add(toman).GT(lim) {
			return errors.Wrapf(types.ErrKycTierExceeded, "redeem KYC tier %d daily limit %s", kycTier, lim)
		}
	}
	// Emergency stepped-redemption brake (network-wide, governance-activated).
	if em := k.GetParams(ctx).EmergencyRedemption; em.Active {
		if capToman, capped := emergencyCapForElapsed(em, ctx.BlockTime().Unix()-em.StartedAt); capped {
			already := k.getCounterUser(ctx, inst.Id, "er", em.StartedAt, holder)
			if already.Add(toman).GT(capToman) {
				return errors.Wrapf(types.ErrRedemptionThrottled,
					"emergency cumulative cap %s Toman (already %s + %s)", capToman, already, toman)
			}
		}
	}
	return nil
}

// addRedeemCounters increments the redeem counters after success (daily/per-user + the emergency
// cumulative bucket keyed by the activation timestamp).
func (k Keeper) addRedeemCounters(ctx sdk.Context, inst types.Institution, holder sdk.AccAddress, toman math.Int) {
	day := dayIndex(ctx)
	k.addCounterTotal(ctx, inst.Id, "rd", day, toman)
	k.addCounterUser(ctx, inst.Id, "ru", day, holder, toman)
	if em := k.GetParams(ctx).EmergencyRedemption; em.Active {
		if _, capped := emergencyCapForElapsed(em, ctx.BlockTime().Unix()-em.StartedAt); capped {
			k.addCounterUser(ctx, inst.Id, "er", em.StartedAt, holder, toman)
		}
	}
}

// kycTierDailyLimit returns the configured daily Toman limit for the holder's KYC tier; zero when
// no matching tier is configured (no extra limit).
func kycTierDailyLimit(p types.InstitutionParams, tier uint32) math.Int {
	for _, kt := range p.KycTierLimits {
		if kt.Tier == tier {
			return types.CapInt(kt.DailyLimitToman)
		}
	}
	return math.ZeroInt()
}

// --- Deposit/redeem idempotency ---

func (k Keeper) depositSeen(ctx sdk.Context, instID, direction, ref string) bool {
	return ctx.KVStore(k.storeKey).Has(types.DepositKey(instID, direction, ref))
}

func (k Keeper) markDeposit(ctx sdk.Context, instID, direction, ref string) {
	ctx.KVStore(k.storeKey).Set(types.DepositKey(instID, direction, ref), []byte{1})
}

// validateParamsTightenOnly enforces the tighten-only rule: the per-tx redeem cap must not fall below the protocol floor.
func (k Keeper) validateParamsTightenOnly(ctx sdk.Context, p types.InstitutionParams) error {
	floor := types.CapInt(k.GetParams(ctx).RedeemFloorPerTx)
	if !floor.IsPositive() {
		return nil
	}
	rpt := types.CapInt(p.Caps.RedeemPerTx)
	if rpt.IsPositive() && rpt.LT(floor) {
		return errors.Wrapf(types.ErrLooserThanFloor, "redeem_per_tx=%s < floor=%s", rpt, floor)
	}
	return nil
}
