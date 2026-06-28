// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

// identityMarkerPrefixes are the raw KV prefixes round-tripped verbatim via GenesisState.StoreEntries
// — issuer single-use attestation nonces and the two-way validator↔DID bindings. They have no
// other structured genesis representation, so exporting the exact bytes guarantees an exact import.
var identityMarkerPrefixes = [][]byte{
	types.IssuerNoncePrefix,
	types.DIDToValidatorPrefix,
	types.ValidatorToDIDPrefix,
}

// InitGenesis loads the initial state.
func (k Keeper) InitGenesis(ctx sdk.Context, gs types.GenesisState) {
	// Re-run structural validation: the JSON-path ValidateGenesis is not guaranteed for
	// programmatically constructed genesis (upgrades, tests, app-wired init).
	if err := gs.Validate(); err != nil {
		panic(err)
	}
	if err := k.SetParams(ctx, gs.Params); err != nil {
		panic(err)
	}
	for _, d := range gs.Identities {
		// SetIdentity also (re)builds the controller→DID secondary index from genesis.
		k.SetIdentity(ctx, d)
		// gs.Validate guarantees a non-empty uniqueness marker for every identity.
		k.setUniqueness(ctx, d.UniquenessHash, d.Did)
	}
	for _, ti := range gs.TrustedIssuers {
		k.SetTrustedIssuer(ctx, ti)
	}
	k.SetIdentityCount(ctx, gs.IdentityCount)
	// Restore the issuer single-use nonces and validator↔DID bindings verbatim; gs.Validate
	// has already confined every key to an allowed identity marker prefix.
	store := ctx.KVStore(k.storeKey)
	for _, e := range gs.StoreEntries {
		store.Set(e.Key, e.Value)
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
	return &types.GenesisState{
		Params:         k.GetParams(ctx),
		Identities:     ids,
		IdentityCount:  k.GetIdentityCount(ctx),
		TrustedIssuers: issuers,
		StoreEntries:   k.exportMarkerEntries(ctx),
	}
}

// exportMarkerEntries collects the raw issuer-nonce and validator↔DID-binding markers.
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
		it.Close()
	}
	return out
}
