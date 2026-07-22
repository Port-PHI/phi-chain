// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"fmt"
	"testing"
	"time"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/coin/keeper"
	"github.com/Port-PHI/phi-chain/x/coin/types"
)

func setupCoinRaw(t *testing.T) (sdk.Context, keeper.Keeper, storetypes.StoreKey) {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_coin_prune"))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	authority := sdk.AccAddress([]byte("gov_authority_______")).String()
	k := keeper.NewKeeper(cdc, key, authority, newFakeBank(), newFakeIdentity())
	require.NoError(t, k.SetParams(testCtx.Ctx, types.DefaultParams()))
	return testCtx.Ctx, k, key
}

func countRange(ctx sdk.Context, key storetypes.StoreKey, start, end []byte) int {
	store := ctx.KVStore(key)
	it := store.Iterator(start, end)
	defer it.Close()
	n := 0
	for ; it.Valid(); it.Next() {
		n++
	}
	return n
}

func dumpMicroQuota(ctx sdk.Context, key storetypes.StoreKey) []string {
	store := ctx.KVStore(key)
	it := storetypes.KVStorePrefixIterator(store, types.MicroQuotaPrefix)
	defer it.Close()
	var out []string
	for ; it.Valid(); it.Next() {
		out = append(out, fmt.Sprintf("%x=%x", it.Key(), it.Value()))
	}
	return out
}

func cutoffDay(ctx sdk.Context) int64 {
	return ctx.BlockTime().Unix()/86400 - types.MicroQuotaRetentionDays
}

// A sweep that runs on the current day must never reset the current day's quota, so an in-progress per-address micro-exemption cap keeps its count even while a large stale backlog drains across blocks.
func TestPruneMicroQuota_SameDayQuotaSurvivesBoundedSweep(t *testing.T) {
	ctx, k, key := setupCoinRaw(t)
	ctx = ctx.WithBlockTime(time.Unix(2_000_000_000, 0))
	nowDay := ctx.BlockTime().Unix() / 86400
	oldDay := nowDay - types.MicroQuotaRetentionDays - 1
	payer := sdk.AccAddress([]byte("live_payer__________")).String()

	k.IncrMicroUsed(ctx, nowDay, payer)
	k.IncrMicroUsed(ctx, nowDay, payer) // count = 2
	const seeded = types.MicroQuotaPruneBudget + 50
	for i := 0; i < seeded; i++ {
		k.IncrMicroUsed(ctx, oldDay, fmt.Sprintf("stale-addr-%05d", i))
	}

	for countRange(ctx, key, types.MicroQuotaPrefix, types.MicroQuotaDayBound(cutoffDay(ctx))) > 0 {
		k.PruneMicroQuota(ctx)
		require.Equal(t, uint64(2), k.GetMicroUsed(ctx, nowDay, payer),
			"the current day's quota must never be reset by a sweep")
	}
	require.Equal(t, uint64(2), k.GetMicroUsed(ctx, nowDay, payer))
}

// With a stale keyset far larger than the budget, each block deletes at most the budget and the backlog drains over multiple blocks — the first rollover block must delete EXACTLY the budget, not the whole set (the alternative would let a day-boundary block do O(keyset) deletes).
func TestPruneMicroQuota_BoundedPerBlockAndDrains(t *testing.T) {
	ctx, k, key := setupCoinRaw(t)
	ctx = ctx.WithBlockTime(time.Unix(2_000_000_000, 0))
	nowDay := ctx.BlockTime().Unix() / 86400
	oldDay := nowDay - types.MicroQuotaRetentionDays - 1

	const seeded = types.MicroQuotaPruneBudget*2 + 37 // 549
	for i := 0; i < seeded; i++ {
		k.IncrMicroUsed(ctx, oldDay, fmt.Sprintf("stale-addr-%05d", i))
	}
	staleBound := types.MicroQuotaDayBound(cutoffDay(ctx))
	require.Equal(t, seeded, countRange(ctx, key, types.MicroQuotaPrefix, staleBound))

	k.PruneMicroQuota(ctx)
	require.Equal(t, seeded-types.MicroQuotaPruneBudget,
		countRange(ctx, key, types.MicroQuotaPrefix, staleBound),
		"exactly budget keys removed on the first rollover block")

	prev := seeded - types.MicroQuotaPruneBudget
	blocks := 1
	for prev > 0 {
		k.PruneMicroQuota(ctx)
		now := countRange(ctx, key, types.MicroQuotaPrefix, staleBound)
		require.LessOrEqual(t, prev-now, types.MicroQuotaPruneBudget, "<= budget deleted per block")
		prev = now
		blocks++
		require.Less(t, blocks, 20, "must drain in a bounded number of blocks")
	}
	require.Zero(t, countRange(ctx, key, types.MicroQuotaPrefix, staleBound), "backlog fully drained")
}

// Two independent keepers seeded identically and stepped through the same blocks must reach byte-identical micro-quota state.
func TestPruneMicroQuota_DeterministicAcrossInstances(t *testing.T) {
	run := func() []string {
		ctx, k, key := setupCoinRaw(t)
		ctx = ctx.WithBlockTime(time.Unix(2_000_000_000, 0))
		nowDay := ctx.BlockTime().Unix() / 86400
		for i := 0; i < 400; i++ {
			day := nowDay - int64(i%10) // days [nowDay-9 .. nowDay]
			k.IncrMicroUsed(ctx, day, fmt.Sprintf("addr-%05d", i))
		}
		base := time.Unix(2_000_000_000, 0)
		for d := 0; d < 4; d++ {
			bctx := ctx.WithBlockTime(base.Add(time.Duration(d) * 24 * time.Hour))
			for b := 0; b < 5; b++ {
				k.PruneMicroQuota(bctx)
			}
		}
		return dumpMicroQuota(ctx, key)
	}
	require.Equal(t, run(), run(), "two independent instances must reach byte-identical micro-quota state")
}
