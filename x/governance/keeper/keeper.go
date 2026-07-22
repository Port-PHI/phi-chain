// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"fmt"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/governance/types"
)

// Keeper stores the governed message-type → vote-path table.
type Keeper struct {
	cdc       codec.BinaryCodec
	storeKey  storetypes.StoreKey
	authority string
}

func NewKeeper(cdc codec.BinaryCodec, storeKey storetypes.StoreKey, authority string) Keeper {
	return Keeper{cdc: cdc, storeKey: storeKey, authority: authority}
}

func (k Keeper) GetAuthority() string { return k.authority }

func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}

func (k Keeper) GetParams(ctx sdk.Context) (p types.Params) {
	bz := ctx.KVStore(k.storeKey).Get(types.ParamsKey)
	if bz == nil {
		return types.DefaultParams()
	}
	k.cdc.MustUnmarshal(bz, &p)
	return p
}

func (k Keeper) SetParams(ctx sdk.Context, p types.Params) error {
	if err := p.Validate(); err != nil {
		return err
	}
	ctx.KVStore(k.storeKey).Set(types.ParamsKey, k.cdc.MustMarshal(&p))
	return nil
}

// VoteRouteTable returns the governed table as a lookup map; the classifier's only state read (the fixed anti-capture rule for the mapping-update message never consults it).
func (k Keeper) VoteRouteTable(ctx sdk.Context) map[string]types.VoteRoute {
	return k.GetParams(ctx).RouteTable()
}

func (k Keeper) InitGenesis(ctx sdk.Context, gs types.GenesisState) {
	if err := gs.Validate(); err != nil {
		panic(err)
	}
	if err := k.SetParams(ctx, gs.Params); err != nil {
		panic(err)
	}
	// Re-check every key at the write: reachable from migration/upgrade paths that skip Validate, and a raw key escaping its prefix could install itself as the params record.
	store := ctx.KVStore(k.storeKey)
	for i, e := range gs.StoreEntries {
		if !types.IsGenesisStoreKey(e.Key) {
			panic(fmt.Sprintf("governance genesis: store_entries[%d]: key %X is not under an exported prefix", i, e.Key))
		}
		store.Set(e.Key, e.Value)
	}
}

// ExportGenesis emits every live store prefix, not only the route table, so an in-flight vote's frozen basis and cast ballots survive a restart.
func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	return &types.GenesisState{
		Params:       k.GetParams(ctx),
		StoreEntries: k.exportStoreEntries(ctx),
	}
}

func (k Keeper) exportStoreEntries(ctx sdk.Context) []types.StoreEntry {
	out := []types.StoreEntry{}
	store := ctx.KVStore(k.storeKey)
	for _, prefix := range types.GenesisStorePrefixes {
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
