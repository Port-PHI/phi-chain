// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"bytes"
	"fmt"

	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

// InitGenesis loads the initial state and validates the solvency invariant at genesis.
func (k Keeper) InitGenesis(ctx sdk.Context, gs types.GenesisState) {
	// Re-run full validation (JSON-path ValidateGenesis is not guaranteed for programmatic genesis); only place with a block time to refuse a future-dated §4.6 timestamp.
	if err := gs.ValidateAtTime(ctx.BlockTime().Unix()); err != nil {
		panic(err)
	}
	if err := k.SetParams(ctx, gs.Params); err != nil {
		panic(err)
	}
	for _, inst := range gs.Institutions {
		// Start §4.6 clock at genesis block time when unset (genesis attested_reserve IS an attestation); explicit value kept for exact round-trip.
		if inst.LastAttestedAt == 0 {
			inst.LastAttestedAt = ctx.BlockTime().Unix()
		}
		k.SetInstitution(ctx, inst)

		// Attribute the genesis attestation to the root admin so a RECORDED attestor exists; mint-separation bars it from the first mint.
		if admin, err := sdk.AccAddressFromBech32(inst.Admin); err == nil {
			k.SetLastAttestor(ctx, inst.Id, admin)
		}
	}
	for _, rg := range gs.RoleGrants {
		addr, err := sdk.AccAddressFromBech32(rg.Address)
		if err != nil {
			panic(err)
		}
		k.SetRole(ctx, rg.Institution, addr, rg.Role)
	}
	for _, req := range gs.FxRequests {
		k.SetFxRequest(ctx, req)
	}
	// Restore markers/counters/approvals verbatim so a round-trip can't replay a deposit, reset a cap, or drop a pending multisig approval.
	if err := k.importStoreEntries(ctx, types.DepositPrefix, gs.DepositMarkers); err != nil {
		panic(err)
	}
	if err := k.importStoreEntries(ctx, types.CounterPrefix, gs.CapCounters); err != nil {
		panic(err)
	}
	if err := k.importStoreEntries(ctx, types.ApprovalPrefix, gs.Approvals); err != nil {
		panic(err)
	}
	if err := k.importResidualEntries(ctx, gs.StoreEntries); err != nil {
		panic(err)
	}
	// The solvency invariant must hold at genesis.
	if msg, broken := AllInvariants(k)(ctx); broken {
		panic(msg)
	}
}

// ExportGenesis collects current state for export.
func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	pending := k.pendingRemovalSet(ctx)

	insts := []types.Institution{}
	k.IterateInstitutions(ctx, func(inst types.Institution) bool {
		insts = append(insts, inst)
		return false
	})
	grants := []types.RoleGrant{}
	k.IterateAllRoles(ctx, func(rg types.RoleGrant) bool {
		if !pending[rg.Institution] {
			grants = append(grants, rg)
		}
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
		Approvals:      k.exportStoreEntriesExcludingPending(ctx, types.ApprovalPrefix, pending),
		StoreEntries:   k.exportResidualEntries(ctx, pending),
	}
}

func (k Keeper) pendingRemovalSet(ctx sdk.Context) map[string]bool {
	out := map[string]bool{}
	it := storetypes.KVStorePrefixIterator(ctx.KVStore(k.storeKey), types.RemovalQueuePrefix)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		if id, ok := types.ParseRemovalQueueKey(it.Key()); ok {
			out[id] = true
		}
	}
	return out
}

func (k Keeper) exportResidualEntries(ctx sdk.Context, pending map[string]bool) []types.StoreEntry {
	out := []types.StoreEntry{}
	for _, prefix := range types.ResidualStorePrefixes {
		if bytes.Equal(prefix, types.HolderKycTierPrefix) {
			out = append(out, k.exportStoreEntriesExcludingPending(ctx, prefix, pending)...)
			continue
		}
		out = append(out, k.exportStoreEntries(ctx, prefix)...)
	}
	return out
}

func (k Keeper) exportStoreEntriesExcludingPending(ctx sdk.Context, prefix []byte, pending map[string]bool) []types.StoreEntry {
	if len(pending) == 0 {
		return k.exportStoreEntries(ctx, prefix)
	}
	out := []types.StoreEntry{}
	it := storetypes.KVStorePrefixIterator(ctx.KVStore(k.storeKey), prefix)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		if id, ok := types.IDFromLenPrefixedKey(it.Key(), prefix); ok && pending[id] {
			continue
		}
		out = append(out, types.StoreEntry{
			Key:   append([]byte(nil), it.Key()...),
			Value: append([]byte(nil), it.Value()...),
		})
	}
	return out
}

func (k Keeper) importResidualEntries(ctx sdk.Context, entries []types.StoreEntry) error {
	store := ctx.KVStore(k.storeKey)
	for i, e := range entries {
		if !types.IsResidualStoreKey(e.Key) {
			return fmt.Errorf("store entry %d: key %X is not under a residual store prefix", i, e.Key)
		}
		store.Set(e.Key, e.Value)
	}
	return nil
}

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

func (k Keeper) importStoreEntries(ctx sdk.Context, prefix []byte, entries []types.StoreEntry) error {
	store := ctx.KVStore(k.storeKey)
	for i, e := range entries {
		// Require at least one byte beyond the prefix; anything outside is refused.
		if len(e.Key) <= len(prefix) || !bytes.HasPrefix(e.Key, prefix) {
			return fmt.Errorf("store entry %d: key %X is not under the expected prefix %X", i, e.Key, prefix)
		}
		store.Set(e.Key, e.Value)
	}
	return nil
}
