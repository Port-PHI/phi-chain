// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"sort"
	"sync"
	"time"

	"cosmossdk.io/log"
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
	authority string // governance address allowed to UpdateParams (usually the gov module)
	// verifier is the phi-crypto port: verifies the issuer attestation and proof-of-possession
	// signatures in RegisterIdentity/RotateIdentityKey. Default build is fail-closed (rejects).
	verifier phicrypto.Verifier
	// elig memoizes the one-human-one-vote quorum-denominator snapshot. It is a shared
	// pointer so every copy of the value-type keeper sees the same cache; it is pure in-memory
	// memoization of a deterministic scan, invalidated on every identity write, so it never affects
	// consensus and is identical across nodes.
	elig *eligibilityCache
}

// eligibilityCache holds the ascending per-controller oldest-active-DID CreatedAt, rebuilt on the
// first read after any identity write. See CountEligibleControllersAt.
type eligibilityCache struct {
	mu     sync.Mutex
	valid  bool
	sorted []int64
}

// NewKeeper creates a new keeper.
func NewKeeper(cdc codec.BinaryCodec, storeKey storetypes.StoreKey, authority string, verifier phicrypto.Verifier) Keeper {
	return Keeper{cdc: cdc, storeKey: storeKey, authority: authority, verifier: verifier, elig: &eligibilityCache{}}
}

// GetAuthority returns the governance address.
func (k Keeper) GetAuthority() string { return k.authority }

// Logger returns the module logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}

// --- Parameters ---

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

// WebAuthnRelyingParty returns the governed WebAuthn relying-party config — the allowed-origin
// allow-list and rpId. The on-chain passkey verifier (gated until the live router) reads
// these from state, so origin/rpId change via governance rather than a binary upgrade.
func (k Keeper) WebAuthnRelyingParty(ctx sdk.Context) (allowedOrigins []string, rpID string) {
	p := k.GetParams(ctx)
	return p.WebauthnAllowedOrigins, p.WebauthnRpId
}

// --- DIDDocument ---

// SetIdentity stores an identity and keeps the controller→DID secondary index consistent (removing a
// stale entry if the controller changed). The index is the source of truth for controller eligibility.
func (k Keeper) SetIdentity(ctx sdk.Context, d types.DIDDocument) {
	store := ctx.KVStore(k.storeKey)
	if prev, found := k.GetIdentity(ctx, d.Did); found && prev.Controller != d.Controller {
		store.Delete(types.ControllerIndexKey(prev.Controller, prev.Did))
	}
	store.Set(types.DIDKey(d.Did), k.cdc.MustMarshal(&d))
	store.Set(types.ControllerIndexKey(d.Controller, d.Did), []byte{1})
	k.invalidateEligibilityCache() // any DID write may change the quorum denominator
}

// invalidateEligibilityCache clears the memoized denominator snapshot after an identity write so the
// next CountEligibleControllersAt rebuilds from current state.
func (k Keeper) invalidateEligibilityCache() {
	k.elig.mu.Lock()
	k.elig.valid = false
	k.elig.sorted = nil
	k.elig.mu.Unlock()
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

// --- Uniqueness marker (one-human-one-DID) ---

// HasUniqueness reports whether this biometric marker has already been used.
func (k Keeper) HasUniqueness(ctx sdk.Context, hash []byte) bool {
	return ctx.KVStore(k.storeKey).Has(types.UniquenessKey(hash))
}

// setUniqueness binds a uniqueness marker to a DID.
func (k Keeper) setUniqueness(ctx sdk.Context, hash []byte, did string) {
	ctx.KVStore(k.storeKey).Set(types.UniquenessKey(hash), []byte(did))
}

// --- Identity counter (bootstrap latch) ---

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

// --- One-human-one-vote tally support ---

// ActiveDIDsAt returns the list of eligible active DIDs at time t:
// status ACTIVE and age >= minAge relative to t (the voting-period start snapshot).
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

// CountEligibleControllersAt counts the DISTINCT eligible controllers at time t — a controller with
// at least one active DID of age >= minAge counts exactly once, no matter how many DIDs it controls.
// This is the one-human-one-vote quorum denominator and MUST share its counting domain with the
// numerator: the governance tally dedups turnout per controller ([IsEligibleControllerAt]), so the
// denominator counts controllers too. Counting DIDs here while deduping votes per controller
// would let many DIDs under one controller inflate the quorum denominator and suppress turnout.
func (k Keeper) CountEligibleControllersAt(ctx sdk.Context, t time.Time, minAge time.Duration) uint64 {
	cutoff := t.Add(-minAge).Unix()
	snap := k.eligibilitySnapshot(ctx)
	// snap is ascending; the count of entries <= cutoff is the smallest index whose value exceeds it.
	return uint64(sort.Search(len(snap), func(i int) bool { return snap[i] > cutoff }))
}

// eligibilitySnapshot returns the ascending list of each controller's OLDEST active-DID CreatedAt
// (one entry per controller with at least one active DID). Counting entries <= cutoff reproduces the
// former O(N) controller-set scan EXACTLY (same one-human-one-vote, voting-start-snapshot semantics)
// while a tally over many concurrent proposals scans the registry once per block instead of once per
// proposal.
//
// The snapshot is memoized ONLY on the deterministic finalize-block path (clear-on-write via
// invalidateEligibilityCache). The sole reader, CountEligibleControllersAt, is called from the gov
// EndBlocker, so the cache it serves is always built from committed finalize state and is identical
// across nodes. CheckTx/ReCheckTx/simulate/query read an uncommitted or ephemeral store; they compute
// a fresh snapshot and never read or seed the shared cache — otherwise a node-local CheckTx view could
// pollute it and feed the EndBlocker a wrong quorum denominator, forking consensus (cross-context
// hardening from independent review). The cache is in-memory only, never persisted to consensus state.
func (k Keeper) eligibilitySnapshot(ctx sdk.Context) []int64 {
	if ctx.ExecMode() != sdk.ExecModeFinalize {
		return k.buildEligibilitySnapshot(ctx)
	}
	k.elig.mu.Lock()
	defer k.elig.mu.Unlock()
	if k.elig.valid {
		return k.elig.sorted
	}
	sorted := k.buildEligibilitySnapshot(ctx)
	k.elig.sorted = sorted
	k.elig.valid = true
	return sorted
}

// buildEligibilitySnapshot scans the registry once and returns the ascending per-controller oldest
// active-DID CreatedAt list. It is a pure function of the store and never touches the cache.
func (k Keeper) buildEligibilitySnapshot(ctx sdk.Context) []int64 {
	minByController := make(map[string]int64)
	k.IterateIdentities(ctx, func(d types.DIDDocument) bool {
		if d.Status == types.DID_STATUS_ACTIVE {
			if cur, ok := minByController[d.Controller]; !ok || d.CreatedAt < cur {
				minByController[d.Controller] = d.CreatedAt
			}
		}
		return false
	})
	sorted := make([]int64, 0, len(minByController))
	for _, v := range minByController {
		sorted = append(sorted, v)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted
}

// IsEligibleControllerAt reports whether a controller address is eligible for one vote at time t:
// an active DID exists with the same controller and age >= minAge (one-human-one-vote). Uses the
// controller→DID secondary index so it scans only that controller's DIDs, not the whole registry.
func (k Keeper) IsEligibleControllerAt(ctx sdk.Context, controller string, t time.Time, minAge time.Duration) bool {
	cutoff := t.Add(-minAge).Unix()
	store := ctx.KVStore(k.storeKey)
	it := storetypes.KVStorePrefixIterator(store, types.ControllerIndexPrefixFor(controller))
	defer it.Close()
	for ; it.Valid(); it.Next() {
		// The index key is ControllerIndexPrefix ‖ controller ‖ 0x00 ‖ did; recover the DID suffix.
		full := it.Key()
		did := string(full[len(types.ControllerIndexPrefixFor(controller)):])
		if d, found := k.GetIdentity(ctx, did); found &&
			d.Status == types.DID_STATUS_ACTIVE && d.CreatedAt <= cutoff {
			return true
		}
	}
	return false
}
