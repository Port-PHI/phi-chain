// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

// InitGenesis loads the initial state and validates the solvency invariant at genesis.
func (k Keeper) InitGenesis(ctx sdk.Context, gs types.GenesisState) {
	// Full structural validation up front: ValidateGenesis on the JSON path is not guaranteed for
	// programmatically constructed genesis (upgrades, tests, app-wired init), so re-run it here.
	if err := gs.Validate(); err != nil {
		panic(err)
	}
	if err := k.SetParams(ctx, gs.Params); err != nil {
		panic(err)
	}
	for _, inst := range gs.Institutions {
		k.SetInstitution(ctx, inst)
	}
	// Sub-institution role grants (RBAC).
	for _, rg := range gs.RoleGrants {
		addr, err := sdk.AccAddressFromBech32(rg.Address)
		if err != nil {
			panic(err)
		}
		k.SetRole(ctx, rg.Institution, addr, rg.Role)
	}
	// Pending fx onboarding requests (guarantor + vote flow).
	for _, req := range gs.FxRequests {
		k.SetFxRequest(ctx, req)
	}
	// Anti-replay markers, cap counters, and accumulated approvals: restored verbatim so an
	// export→import round-trip cannot replay a processed deposit, reset a daily cap, or drop a
	// pending multisig approval.
	k.importStoreEntries(ctx, gs.DepositMarkers)
	k.importStoreEntries(ctx, gs.CapCounters)
	k.importStoreEntries(ctx, gs.Approvals)
	// The initial invariant must hold; otherwise genesis is invalid.
	if msg, broken := AllInvariants(k)(ctx); broken {
		panic(msg)
	}
}

// ExportGenesis collects the current state for export.
func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	insts := []types.Institution{}
	k.IterateInstitutions(ctx, func(inst types.Institution) bool {
		insts = append(insts, inst)
		return false
	})
	grants := []types.RoleGrant{}
	k.IterateAllRoles(ctx, func(rg types.RoleGrant) bool {
		grants = append(grants, rg)
		return false
	})
	fxReqs := []types.FxEntryRequest{}
	k.IterateFxRequests(ctx, func(req types.FxEntryRequest) bool {
		fxReqs = append(fxReqs, req)
		return false
	})
	return &types.GenesisState{
		Params:         k.GetParams(ctx),
		Institutions:   insts,
		RoleGrants:     grants,
		FxRequests:     fxReqs,
		DepositMarkers: k.exportStoreEntries(ctx, types.DepositPrefix),
		CapCounters:    k.exportStoreEntries(ctx, types.CounterPrefix),
		Approvals:      k.exportStoreEntries(ctx, types.ApprovalPrefix),
	}
}

// exportStoreEntries returns every (full key, value) record under a store prefix. The key includes
// the prefix byte, so importStoreEntries restores the record at the exact same location — the markers
// are opaque KV state with no other structured form, so round-tripping the raw bytes is exact.
func (k Keeper) exportStoreEntries(ctx sdk.Context, prefix []byte) []types.StoreEntry {
	out := []types.StoreEntry{}
	store := ctx.KVStore(k.storeKey)
	it := storetypes.KVStorePrefixIterator(store, prefix)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		out = append(out, types.StoreEntry{
			Key:   append([]byte(nil), it.Key()...),
			Value: append([]byte(nil), it.Value()...),
		})
	}
	return out
}

// importStoreEntries writes raw KV records back verbatim (inverse of exportStoreEntries).
func (k Keeper) importStoreEntries(ctx sdk.Context, entries []types.StoreEntry) {
	store := ctx.KVStore(k.storeKey)
	for _, e := range entries {
		store.Set(e.Key, e.Value)
	}
}
