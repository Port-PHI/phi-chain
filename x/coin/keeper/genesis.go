// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"fmt"

	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/coin/types"
)

// InitGenesis loads the initial state.
func (k Keeper) InitGenesis(ctx sdk.Context, gs types.GenesisState) {
	// Re-run structural validation; the JSON-path ValidateGenesis is not guaranteed for programmatically constructed genesis (upgrades, tests, app-wired init).
	if err := gs.Validate(); err != nil {
		panic(err)
	}
	if err := k.SetParams(ctx, gs.Params); err != nil {
		panic(err)
	}
	for _, ca := range gs.CoinAges {
		k.SetCoinAge(ctx, ca)
	}
	// Written verbatim, which is what makes the import exact.
	store := ctx.KVStore(k.storeKey)
	for i, e := range gs.StoreEntries {
		if !types.IsGenesisStoreKey(e.Key) {
			panic(fmt.Sprintf("coin genesis: store_entries[%d]: key %X is not under the micro-quota prefix", i, e.Key))
		}
		store.Set(e.Key, e.Value)
	}
}

// ExportGenesis collects the current state for export.
func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	ages := []types.CoinAge{}
	k.IterateCoinAges(ctx, func(ca types.CoinAge) bool {
		ages = append(ages, ca)
		return false
	})

	entries := []types.StoreEntry{}
	it := storetypes.KVStorePrefixIterator(ctx.KVStore(k.storeKey), types.MicroQuotaPrefix)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		entries = append(entries, types.StoreEntry{
			Key:   append([]byte(nil), it.Key()...),
			Value: append([]byte(nil), it.Value()...),
		})
	}

	return &types.GenesisState{
		Params:       k.GetParams(ctx),
		CoinAges:     ages,
		StoreEntries: entries,
	}
}
