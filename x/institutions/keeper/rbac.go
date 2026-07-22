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

// RBAC, aggregated multisig approval, daily caps, and deposit/redeem idempotency.

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

// IterateAllRoles iterates over all role grants of all institutions.
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

func (k Keeper) effectiveRole(ctx sdk.Context, inst types.Institution, signer sdk.AccAddress) types.InstitutionRole {
	if signer.String() == inst.Admin {
		return types.INSTITUTION_ROLE_ADMIN
	}
	return k.GetRole(ctx, inst.Id, signer)
}

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

func (k Keeper) countAdmins(ctx sdk.Context, inst types.Institution) uint32 {
	set := map[string]bool{}
	add := func(addr string) {
		if _, err := sdk.AccAddressFromBech32(addr); err == nil {
			set[addr] = true
		}
	}
	add(inst.Admin)
	k.IterateRolesFor(ctx, inst.Id, func(rg types.RoleGrant) bool {
		if rg.Role == types.INSTITUTION_ROLE_ADMIN {
			add(rg.Address)
		}
		return false
	})
	return uint32(len(set))
}

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

func (k Keeper) adminEpoch(ctx sdk.Context, instID string) uint64 {
	bz := ctx.KVStore(k.storeKey).Get(types.AdminEpochKey(instID))
	if len(bz) != 8 {
		return 0
	}
	return binary.BigEndian.Uint64(bz)
}

func (k Keeper) bumpAdminEpoch(ctx sdk.Context, instID string) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], k.adminEpoch(ctx, instID)+1)
	ctx.KVStore(k.storeKey).Set(types.AdminEpochKey(instID), b[:])
}

func (k Keeper) recordApproval(ctx sdk.Context, inst types.Institution, contentHash []byte, signer sdk.AccAddress) uint32 {
	var epoch [8]byte
	binary.BigEndian.PutUint64(epoch[:], k.adminEpoch(ctx, inst.Id))
	ctx.KVStore(k.storeKey).Set(types.ApprovalKey(inst.Id, contentHash, signer), epoch[:])
	return k.countApprovals(ctx, inst, contentHash)
}

func (k Keeper) countApprovals(ctx sdk.Context, inst types.Institution, contentHash []byte) uint32 {
	store := ctx.KVStore(k.storeKey)
	prefix := types.ApprovalPrefixFor(inst.Id, contentHash)
	epoch := k.adminEpoch(ctx, inst.Id)
	it := storetypes.KVStorePrefixIterator(store, prefix)
	defer it.Close()
	var n uint32
	for ; it.Valid(); it.Next() {
		if len(it.Value()) != 8 || binary.BigEndian.Uint64(it.Value()) != epoch {
			continue
		}
		signer := sdk.AccAddress(it.Key()[len(prefix):])
		if k.effectiveRole(ctx, inst, signer) != types.INSTITUTION_ROLE_ADMIN {
			continue
		}
		n++
	}
	return n
}

func (k Keeper) clearApprovals(ctx sdk.Context, instID string, contentHash []byte) {
	store := ctx.KVStore(k.storeKey)
	it := storetypes.KVStorePrefixIterator(store, types.ApprovalPrefixFor(instID, contentHash))
	var keys [][]byte
	for ; it.Valid(); it.Next() {
		keys = append(keys, append([]byte(nil), it.Key()...))
	}
	_ = it.Close()
	for _, key := range keys {
		store.Delete(key)
	}
}

func lengthPrefixed(parts ...[]byte) []byte {
	n := 4 * len(parts)
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 0, n)
	var lenbuf [4]byte
	for _, p := range parts {
		binary.BigEndian.PutUint32(lenbuf[:], uint32(len(p)))
		out = append(out, lenbuf[:]...)
		out = append(out, p...)
	}
	return out
}

func contentHashOf(parts ...[]byte) []byte {
	sum := sha256.Sum256(lengthPrefixed(parts...))
	return sum[:]
}

func roleBytes(r types.InstitutionRole) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(r))
	return b[:]
}

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

func (k Keeper) enforceMintCaps(ctx sdk.Context, inst types.Institution, recipient sdk.AccAddress, toman math.Int, kycTier uint32) error {
	c := inst.Params.Caps
	day := dayIndex(ctx)
	// Protocol mint ceiling: hard upper bound on every mint even with no institution cap.
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
	// Recipient's KYC daily limit, resolved FAIL-CLOSED: an unconfigured tier falls to the strictest configured limit (never "no limit").
	if lim := k.effectiveMintKycDailyLimit(ctx, inst, recipient, kycTier); lim.IsPositive() {
		if k.getCounterUser(ctx, inst.Id, "mu", day, recipient).Add(toman).GT(lim) {
			return errors.Wrapf(types.ErrKycTierExceeded, "mint KYC tier %d daily limit %s", kycTier, lim)
		}
	}
	return nil
}

func (k Keeper) addMintCounters(ctx sdk.Context, inst types.Institution, recipient sdk.AccAddress, toman math.Int) {
	day := dayIndex(ctx)
	k.addCounterTotal(ctx, inst.Id, "md", day, toman)
	k.addCounterUser(ctx, inst.Id, "mu", day, recipient, toman)
}

func (k Keeper) redeemSubject(ctx sdk.Context, holder sdk.AccAddress) (kind byte, subject string) {
	if did, ok := k.identityKeeper.SubjectDID(ctx, holder.String()); ok && did != "" {
		return types.RedeemSubjectDID, did
	}
	return types.RedeemSubjectUnidentified, types.UnidentifiedRedeemSubject
}

func (k Keeper) withinFloorReservation(ctx sdk.Context, holder sdk.AccAddress, uphi math.Int) bool {
	mp := k.GetParams(ctx)
	floorToman := types.CapInt(mp.RedeemFloorPerTx)
	if !floorToman.IsPositive() {
		return false
	}
	floorUphi, integral := MintedUphiForToman(floorToman, mp.PhiToToman)
	if !integral || !floorUphi.IsPositive() {
		return false
	}
	kind, subject := k.redeemSubject(ctx, holder)
	return k.getRedeemSubjectTotal(ctx, dayIndex(ctx), kind, subject).Add(uphi).LTE(floorUphi)
}

func (k Keeper) getRedeemSubjectTotal(ctx sdk.Context, day int64, kind byte, subject string) math.Int {
	return parseCounter(ctx.KVStore(k.storeKey).Get(types.RedeemSubjectCounterKey(day, kind, subject)))
}

func (k Keeper) addRedeemSubjectTotal(ctx sdk.Context, day int64, kind byte, subject string, uphi math.Int) {
	key := types.RedeemSubjectCounterKey(day, kind, subject)
	v := k.getRedeemSubjectTotal(ctx, day, kind, subject).Add(uphi)
	ctx.KVStore(k.storeKey).Set(key, []byte(v.String()))
}

func (k Keeper) enforceRedeemCaps(ctx sdk.Context, inst types.Institution, holder sdk.AccAddress, toman, uphi math.Int) error {
	c := inst.Params.Caps
	day := dayIndex(ctx)
	// Every redeem cap read through the CURRENT floor, so a governance raise reaches institutions registered under the old floor.
	floor := types.CapInt(k.GetParams(ctx).RedeemFloorPerTx)
	if lim := types.AtLeastFloor(types.CapInt(c.RedeemPerTx), floor); lim.IsPositive() && toman.GT(lim) {
		return errors.Wrapf(types.ErrCapExceeded, "redeem per_tx: %s > %s", toman, lim)
	}
	if lim := types.AtLeastFloor(types.CapInt(c.RedeemDaily), floor); lim.IsPositive() {
		if k.getCounterTotal(ctx, inst.Id, "rd", day).Add(toman).GT(lim) &&
			!k.withinFloorReservation(ctx, holder, uphi) {
			return errors.Wrapf(types.ErrCapExceeded, "redeem daily cap %s", lim)
		}
	}
	if lim := types.AtLeastFloor(types.CapInt(c.RedeemPerUser), floor); lim.IsPositive() {
		if k.getCounterUser(ctx, inst.Id, "ru", day, holder).Add(toman).GT(lim) {
			return errors.Wrapf(types.ErrCapExceeded, "redeem per_user cap %s", lim)
		}
	}
	// KYC-tier daily limit read from COMPLIANCE-gated state (never the tx); unassigned holder gets the strictest configured limit.
	if lim := types.AtLeastFloor(k.effectiveKycDailyLimit(ctx, inst, holder), floor); lim.IsPositive() {
		if k.getCounterUser(ctx, inst.Id, "ru", day, holder).Add(toman).GT(lim) {
			return errors.Wrapf(types.ErrKycTierExceeded, "redeem KYC tier daily limit %s", lim)
		}
	}
	// Network-wide per-DID daily cap (holder's total across all institutions today).
	if lim := types.AtLeastFloor(
		types.CapInt(k.GetParams(ctx).RedeemDailyCapPerDidUphi),
		k.GetParams(ctx).RedeemFloorUphi(),
	); lim.IsPositive() {
		kind, subject := k.redeemSubject(ctx, holder)
		if k.getRedeemSubjectTotal(ctx, day, kind, subject).Add(uphi).GT(lim) {
			return errors.Wrapf(types.ErrCapExceeded,
				"network-wide per-DID daily redeem cap %s uphi (already %s + %s across all institutions)",
				lim, k.getRedeemSubjectTotal(ctx, day, kind, subject), uphi)
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

func (k Keeper) addRedeemCounters(ctx sdk.Context, inst types.Institution, holder sdk.AccAddress, toman, uphi math.Int) {
	day := dayIndex(ctx)
	k.addCounterTotal(ctx, inst.Id, "rd", day, toman)
	k.addCounterUser(ctx, inst.Id, "ru", day, holder, toman)
	kind, subject := k.redeemSubject(ctx, holder)
	k.addRedeemSubjectTotal(ctx, day, kind, subject, uphi)
	if em := k.GetParams(ctx).EmergencyRedemption; em.Active {
		if _, capped := emergencyCapForElapsed(em, ctx.BlockTime().Unix()-em.StartedAt); capped {
			k.addCounterUser(ctx, inst.Id, "er", em.StartedAt, holder, toman)
		}
	}
}

func kycTierDailyLimit(p types.InstitutionParams, tier uint32) math.Int {
	for _, kt := range p.KycTierLimits {
		if kt.Tier == tier {
			return types.CapInt(kt.DailyLimitToman)
		}
	}
	return math.ZeroInt()
}

func (k Keeper) depositSeen(ctx sdk.Context, instID, direction, ref string) bool {
	return ctx.KVStore(k.storeKey).Has(types.DepositKey(instID, direction, ref))
}

func (k Keeper) markDeposit(ctx sdk.Context, instID, direction, ref string) {
	ctx.KVStore(k.storeKey).Set(types.DepositKey(instID, direction, ref), []byte{types.DepositMarkerByte})
}

func (k Keeper) validateParamsTightenOnly(ctx sdk.Context, p types.InstitutionParams) error {
	mp := k.GetParams(ctx)

	// No redeem cap (base caps + KYC tier limits) may fall below the protocol floor.
	floor := types.CapInt(mp.RedeemFloorPerTx)
	if name, v, below := p.RedeemCapsBelowFloor(floor); below {
		return errors.Wrapf(types.ErrLooserThanFloor, "%s=%s < floor=%s", name, v, floor)
	}

	// Institution's attestation-staleness limit must not exceed the protocol ceiling (smaller is honoured).
	if ceil := mp.MaxAttestationStalenessSeconds; ceil > 0 {
		if lat := p.AutoSuspendRules.MaxVaultAttestationLatencyS; lat > ceil {
			return errors.Wrapf(types.ErrLooserThanFloor,
				"max_vault_attestation_latency_s=%d > protocol max_attestation_staleness_seconds=%d", lat, ceil)
		}
	}
	return nil
}
