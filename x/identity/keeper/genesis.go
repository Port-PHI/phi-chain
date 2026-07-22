// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"bytes"

	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

var identityMarkerPrefixes = [][]byte{
	types.IssuerNoncePrefix,
	types.RecoveryNoncePrefix,
	types.DIDToValidatorPrefix,
	types.ValidatorToDIDPrefix,
	types.GuardianEpochPrefix,
	types.RecoveryTallyEpochPrefix,
	// Carried only for eligible_since (not derivable); oldest created_at is rebuilt from the replay.
	types.ControllerEligibilityPrefix,
}

// InitGenesis loads the initial state.
func (k Keeper) InitGenesis(ctx sdk.Context, gs types.GenesisState) {
	// Re-run structural validation: not guaranteed for programmatically constructed genesis.
	if err := gs.Validate(); err != nil {
		panic(err)
	}
	if err := k.SetParams(ctx, gs.Params); err != nil {
		panic(err)
	}
	for _, d := range gs.Identities {
		k.SetIdentity(ctx, d)
		k.setUniqueness(ctx, d.UniquenessHash, d.Did)
	}
	for _, ti := range gs.TrustedIssuers {
		k.SetTrustedIssuer(ctx, ti)
	}
	// Guardian identities are NOT checked here (commitments are hiding); enforced at approval time.
	for _, g := range gs.GuardianSets {
		k.SetGuardianSet(ctx, g)
	}
	// execute_after / expires_at are absolute, so import never restarts an opposition window.
	for _, r := range gs.RecoveryRequests {
		k.SetRecoveryRequest(ctx, r)
		k.markRecoveryNonce(ctx, r.Did, r.Nonce)
	}
	k.SetIdentityCount(ctx, gs.IdentityCount)
	// Restore markers verbatim; eligibility records are applied afterwards (only eligible_since carried).
	store := ctx.KVStore(k.storeKey)
	for _, e := range gs.StoreEntries {
		if bytes.HasPrefix(e.Key, types.ControllerEligibilityPrefix) {
			continue
		}
		store.Set(e.Key, e.Value)
	}
	k.restoreEligibleSince(ctx, gs.StoreEntries)
}

func (k Keeper) restoreEligibleSince(ctx sdk.Context, entries []types.StoreEntry) {
	store := ctx.KVStore(k.storeKey)
	for _, e := range entries {
		if !bytes.HasPrefix(e.Key, types.ControllerEligibilityPrefix) {
			continue
		}
		_, since, ok := types.DecodeControllerEligibility(e.Value)
		if !ok || since == 0 {
			continue
		}
		rebuilt := store.Get(e.Key)
		oldest, _, live := types.DecodeControllerEligibility(rebuilt)
		if !live {
			continue
		}
		store.Set(e.Key, types.EncodeControllerEligibility(oldest, since))
	}
}

// ExportGenesis collects the current state for export.
func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	ids := []types.DIDDocument{}
	k.IterateIdentities(ctx, func(d types.DIDDocument) bool {
		ids = append(ids, d)
		return false
	})
	issuers := []types.TrustedIssuer{}
	k.IterateTrustedIssuers(ctx, func(ti types.TrustedIssuer) bool {
		issuers = append(issuers, ti)
		return false
	})
	guardianSets := []types.GuardianSet{}
	k.IterateGuardians(ctx, func(g types.GuardianSet) bool {
		guardianSets = append(guardianSets, g)
		return false
	})
	// Only PENDING requests are exported; terminal ones are settled and carry no live meaning.
	recoveries := []types.RecoveryRequest{}
	k.IterateRecoveryRequests(ctx, func(r types.RecoveryRequest) bool {
		if r.Status == types.RECOVERY_STATUS_PENDING {
			recoveries = append(recoveries, r)
		}
		return false
	})
	return &types.GenesisState{
		Params:           k.GetParams(ctx),
		Identities:       ids,
		IdentityCount:    k.GetIdentityCount(ctx),
		TrustedIssuers:   issuers,
		StoreEntries:     k.exportMarkerEntries(ctx),
		GuardianSets:     guardianSets,
		RecoveryRequests: recoveries,
	}
}

func (k Keeper) exportMarkerEntries(ctx sdk.Context) []types.StoreEntry {
	out := []types.StoreEntry{}
	store := ctx.KVStore(k.storeKey)
	for _, prefix := range identityMarkerPrefixes {
		it := storetypes.KVStorePrefixIterator(store, prefix)
		for ; it.Valid(); it.Next() {
			out = append(out, types.StoreEntry{
				Key:   append([]byte(nil), it.Key()...),
				Value: append([]byte(nil), it.Value()...),
			})
		}
		_ = it.Close()
	}
	return out
}
