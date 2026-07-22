// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/internal/storeprefix/prefixtest"
	"github.com/Port-PHI/phi-chain/x/coin/types"
)

// TestGenesis_RoundTripsEveryDeclaredStorePrefix seeds a record under every declared prefix through the real keeper writers and asserts one export→import cycle reproduces the module's whole keyspace.
func TestGenesis_RoundTripsEveryDeclaredStorePrefix(t *testing.T) {
	ctx, k, key := setupCoinRaw(t)
	ctx = ctx.WithBlockTime(time.Unix(1_700_000_000, 0))
	day := ctx.BlockTime().Unix() / 86400

	owner := sdk.AccAddress([]byte("coin-age-owner______")).String()
	k.SetCoinAge(ctx, types.CoinAge{
		Address: owner,
		Lots: []types.CoinLot{
			{Amount: "1000000", AcquiredAt: ctx.BlockTime().Unix() - 86400},
			{Amount: "2000000", AcquiredAt: ctx.BlockTime().Unix()},
		},
	})
	k.IncrMicroUsed(ctx, day, owner)

	before := prefixtest.Dump(ctx, key)
	prefixtest.RequireSeeded(t, before, types.AllStorePrefixes())

	exported := k.ExportGenesis(ctx)
	require.NoError(t, exported.Validate())

	ctx2, k2, key2 := setupCoinRaw(t)
	ctx2 = ctx2.WithBlockTime(ctx.BlockTime())
	require.NotPanics(t, func() { k2.InitGenesis(ctx2, *exported) })

	prefixtest.RequireRoundTrip(t, types.AllStorePrefixes(), before, prefixtest.Dump(ctx2, key2))
}
