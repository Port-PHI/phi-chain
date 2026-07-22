// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"encoding/binary"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/phicrypto"
	"github.com/Port-PHI/phi-chain/x/identity/types"
)

// Keeper manages the state of the identity module.
type Keeper struct {
	cdc       codec.BinaryCodec
	storeKey  storetypes.StoreKey
	authority string // governance address allowed to UpdateParams
	// verifier is the phi-crypto port for attestation/PoP signatures; default build is fail-closed.
	verifier phicrypto.Verifier
	// bankKeeper moves the social-recovery deposit only; every movement is supply-neutral (never burned).
	bankKeeper types.BankKeeper
}

// NewKeeper creates a new keeper.
func NewKeeper(cdc codec.BinaryCodec, storeKey storetypes.StoreKey, authority string, verifier phicrypto.Verifier, bankKeeper types.BankKeeper) Keeper {
	return Keeper{
		cdc:        cdc,
		storeKey:   storeKey,
		authority:  authority,
		verifier:   verifier,
		bankKeeper: bankKeeper,
	}
}

// GetAuthority returns the governance address.
func (k Keeper) GetAuthority() string { return k.authority }

// Logger returns the module logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}

// GetParams returns the current parameters.
func (k Keeper) GetParams(ctx sdk.Context) (p types.Params) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.ParamsKey)
	if bz == nil {
		return types.DefaultParams()
	}
	k.cdc.MustUnmarshal(bz, &p)
	return p
}

// SetParams stores the parameters (after validation).
func (k Keeper) SetParams(ctx sdk.Context, p types.Params) error {
	if err := p.Validate(); err != nil {
		return err
	}
	store := ctx.KVStore(k.storeKey)
	store.Set(types.ParamsKey, k.cdc.MustMarshal(&p))
	return nil
}

// MinIdentityAge returns the minimum DID age as a Duration.
func (k Keeper) MinIdentityAge(ctx sdk.Context) time.Duration {
	return time.Duration(k.GetParams(ctx).MinIdentityAgeSeconds) * time.Second
}

// UVPolicy returns the governed stepped User-Verification policy: the message type URLs that make a transaction sensitive, and the transfer amount (uphi) at or above which a coin transfer is also sensitive (zero disables the amount rule).
func (k Keeper) UVPolicy(ctx sdk.Context) (sensitiveMsgTypeURLs []string, largeTransferUphi math.Int) {
	p := k.GetParams(ctx)
	return p.UvSensitiveMsgTypeUrls, p.UVLargeTransferAmount()
}

// WebAuthnRelyingParty returns the governed WebAuthn relying-party config — the allowed-origin allow-list and rpId.
func (k Keeper) WebAuthnRelyingParty(ctx sdk.Context) (allowedOrigins []string, rpID string) {
	p := k.GetParams(ctx)
	return p.WebauthnAllowedOrigins, p.WebauthnRpId
}

// SetIdentity stores an identity and keeps the derived indexes consistent: the controller→DID secondary index (removing a stale entry if the controller changed) and the one-human-one-vote eligibility records.
func (k Keeper) SetIdentity(ctx sdk.Context, d types.DIDDocument) {
	store := ctx.KVStore(k.storeKey)
	prevController := ""
	if prev, found := k.GetIdentity(ctx, d.Did); found && prev.Controller != d.Controller {
		store.Delete(types.ControllerIndexKey(prev.Controller, prev.Did))
		prevController = prev.Controller
	}
	store.Set(types.DIDKey(d.Did), k.cdc.MustMarshal(&d))
	store.Set(types.ControllerIndexKey(d.Controller, d.Did), []byte{1})

	k.refreshControllerEligibility(ctx, d.Controller)
	k.refreshControllerSweepStatus(ctx, d.Controller)
	if prevController != "" {
		k.refreshControllerEligibility(ctx, prevController)
		k.refreshControllerSweepStatus(ctx, prevController)
	}
}

// GetIdentity reads an identity by its DID.
func (k Keeper) GetIdentity(ctx sdk.Context, did string) (types.DIDDocument, bool) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.DIDKey(did))
	if bz == nil {
		return types.DIDDocument{}, false
	}
	var d types.DIDDocument
	k.cdc.MustUnmarshal(bz, &d)
	return d, true
}

// HasIdentity checks whether a DID exists.
func (k Keeper) HasIdentity(ctx sdk.Context, did string) bool {
	return ctx.KVStore(k.storeKey).Has(types.DIDKey(did))
}

// HasNonActiveDID reports whether the controller address controls any DID whose status is not ACTIVE (SUSPENDED or REVOKED).
func (k Keeper) HasNonActiveDID(ctx sdk.Context, controller string) bool {
	store := ctx.KVStore(k.storeKey)
	prefix := types.ControllerIndexPrefixFor(controller)
	it := storetypes.KVStorePrefixIterator(store, prefix)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		did := string(it.Key()[len(prefix):])
		if d, found := k.GetIdentity(ctx, did); found && d.Status != types.DID_STATUS_ACTIVE {
			return true
		}
	}
	return false
}

// IterateIdentities iterates over all identities; returning true stops the loop.
func (k Keeper) IterateIdentities(ctx sdk.Context, cb func(types.DIDDocument) bool) {
	store := ctx.KVStore(k.storeKey)
	it := storetypes.KVStorePrefixIterator(store, types.DIDPrefix)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var d types.DIDDocument
		k.cdc.MustUnmarshal(it.Value(), &d)
		if cb(d) {
			break
		}
	}
}

// HasUniqueness reports whether this biometric marker has already been used.
func (k Keeper) HasUniqueness(ctx sdk.Context, hash []byte) bool {
	return ctx.KVStore(k.storeKey).Has(types.UniquenessKey(hash))
}

func (k Keeper) setUniqueness(ctx sdk.Context, hash []byte, did string) {
	ctx.KVStore(k.storeKey).Set(types.UniquenessKey(hash), []byte(did))
}

// GetIdentityCount returns the total identity counter.
func (k Keeper) GetIdentityCount(ctx sdk.Context) uint64 {
	bz := ctx.KVStore(k.storeKey).Get(types.IdentityCountKey)
	if bz == nil {
		return 0
	}
	return sdk.BigEndianToUint64(bz)
}

// SetIdentityCount sets the counter.
func (k Keeper) SetIdentityCount(ctx sdk.Context, c uint64) {
	ctx.KVStore(k.storeKey).Set(types.IdentityCountKey, sdk.Uint64ToBigEndian(c))
}

// BootstrapPhase reports the bootstrap phase status.
func (k Keeper) BootstrapPhase(ctx sdk.Context) bool {
	return k.GetParams(ctx).BootstrapPhase
}

// ActiveDIDsAt returns the list of eligible active DIDs at time t: status ACTIVE and age >= minAge relative to t (the voting-period start snapshot).
func (k Keeper) ActiveDIDsAt(ctx sdk.Context, t time.Time, minAge time.Duration) []types.DIDDocument {
	cutoff := t.Add(-minAge).Unix()
	var out []types.DIDDocument
	k.IterateIdentities(ctx, func(d types.DIDDocument) bool {
		if d.Status == types.DID_STATUS_ACTIVE && d.CreatedAt <= cutoff {
			out = append(out, d)
		}
		return false
	})
	return out
}

// CountEligibleControllersAt counts the DISTINCT eligible controllers at time t — a controller with at least one active DID of age >= minAge counts exactly once, no matter how many DIDs it controls.
func (k Keeper) CountEligibleControllersAt(ctx sdk.Context, t time.Time, minAge time.Duration) uint64 {
	cutoff := t.Add(-minAge).Unix()
	total := k.EligibleControllerTotal(ctx)
	tail, truncated := k.countEligibleNewerThan(ctx, cutoff)

	if truncated {
		// The tail was cut short, so the denominator below is larger than the true one.
		k.Logger(ctx).Error("eligibility tail scan truncated; quorum denominator is an over-estimate",
			"cutoff", cutoff, "scanned", tail, "ceiling", MaxEligibilityTailScan)
	}

	if tail > total {
		// The tail cannot honestly exceed the total: it counts a subset of the very controllers the total counts.
		return total
	}
	// tail == total is not drift.
	return total - tail
}

// MaxEligibilityTailScan is the hard ceiling on how many age-ordered entries countEligibleNewerThan will examine in one call.
const MaxEligibilityTailScan = 5_000_000

func (k Keeper) countEligibleNewerThan(ctx sdk.Context, cutoff int64) (count uint64, truncated bool) {
	return k.countEligibleNewerThanCapped(ctx, cutoff, MaxEligibilityTailScan)
}

func (k Keeper) countEligibleNewerThanCapped(ctx sdk.Context, cutoff int64, ceiling uint64) (count uint64, truncated bool) {
	it := storetypes.KVStoreReversePrefixIterator(ctx.KVStore(k.storeKey), types.EligibilityByAgePrefix)
	defer it.Close()
	var n uint64
	for ; it.Valid(); it.Next() {
		if n >= ceiling {
			return n, true
		}
		createdAt, ok := types.CreatedAtFromEligibilityByAgeKey(it.Key())
		if !ok || createdAt <= cutoff {
			break
		}
		n++
	}
	return n, false
}

// EligibleControllerTotal returns the number of controllers holding at least one non-revoked DID.
func (k Keeper) EligibleControllerTotal(ctx sdk.Context) uint64 {
	bz := ctx.KVStore(k.storeKey).Get(types.EligibleControllerTotalKey)
	if bz == nil {
		return 0
	}
	return binary.BigEndian.Uint64(bz)
}

func (k Keeper) setEligibleControllerTotal(ctx sdk.Context, n uint64) {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, n)
	ctx.KVStore(k.storeKey).Set(types.EligibleControllerTotalKey, b)
}

func (k Keeper) refreshControllerEligibility(ctx sdk.Context, controller string) {
	store := ctx.KVStore(k.storeKey)

	oldest, has := int64(0), false
	prefix := types.ControllerIndexPrefixFor(controller)
	it := storetypes.KVStorePrefixIterator(store, prefix)
	for ; it.Valid(); it.Next() {
		did := string(it.Key()[len(prefix):])
		if d, found := k.GetIdentity(ctx, did); found && d.Status != types.DID_STATUS_REVOKED {
			if !has || d.CreatedAt < oldest {
				oldest, has = d.CreatedAt, true
			}
		}
	}
	// Closed explicitly: the reconciliation below writes to the same store.
	_ = it.Close()

	recordKey := types.ControllerEligibilityKey(controller)
	prevBz := store.Get(recordKey)
	prevOldest, prevSince, hadRecord := types.DecodeControllerEligibility(prevBz)

	switch {
	case hadRecord && !has:
		// The controller lost its last NON-REVOKED DID: every DID it holds is now REVOKED (terminal), so it can never be eligible again on that identity.
		store.Delete(types.EligibilityByAgeKey(prevOldest, controller))
		store.Delete(recordKey)
		k.setEligibleControllerTotal(ctx, k.EligibleControllerTotal(ctx)-1)
	case !hadRecord && has:
		// The controller gained its first standing DID — or regained one after every prior DID was revoked.
		store.Set(recordKey, types.EncodeControllerEligibility(oldest, ctx.BlockTime().Unix()))
		store.Set(types.EligibilityByAgeKey(oldest, controller), []byte{1})
		k.setEligibleControllerTotal(ctx, k.EligibleControllerTotal(ctx)+1)
	case hadRecord && has:
		// Still standing, and CONTINUOUSLY so — but "continuously in the basis" is not the same as "no better off than it was", and only the latter is what a frozen basis may be judged against.
		if prevOldest != oldest {
			since := prevSince
			if oldest < prevOldest {
				since = ctx.BlockTime().Unix()
			}
			store.Delete(types.EligibilityByAgeKey(prevOldest, controller))
			store.Set(recordKey, types.EncodeControllerEligibility(oldest, since))
			store.Set(types.EligibilityByAgeKey(oldest, controller), []byte{1})
		}
	}
}

// IsEligibleControllerAt reports whether a controller address belongs to the eligible set at time t: a NON-REVOKED (ACTIVE or SUSPENDED) DID exists under that controller with age >= minAge (one-human-one-vote).
func (k Keeper) IsEligibleControllerAt(ctx sdk.Context, controller string, t time.Time, minAge time.Duration) bool {
	oldest, _, ok := types.DecodeControllerEligibility(
		ctx.KVStore(k.storeKey).Get(types.ControllerEligibilityKey(controller)))
	return ok && oldest <= t.Add(-minAge).Unix()
}

// IsEligibleControllerSince reports whether a controller belongs to an eligibility basis frozen at `since` with cutoff `t` — the exact membership test the frozen QUORUM DENOMINATOR was counted with.
func (k Keeper) IsEligibleControllerSince(ctx sdk.Context, controller string, t time.Time, minAge time.Duration, since time.Time) bool {
	oldest, eligibleSince, ok := types.DecodeControllerEligibility(
		ctx.KVStore(k.storeKey).Get(types.ControllerEligibilityKey(controller)))
	if !ok || oldest > t.Add(-minAge).Unix() {
		return false
	}
	if since.Unix() == 0 {
		return true
	}
	return eligibleSince <= since.Unix()
}
