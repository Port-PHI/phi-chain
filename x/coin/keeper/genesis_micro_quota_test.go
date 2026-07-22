// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/coin/types"
)

// TestGenesis_MicroExemptionQuotaSurvivesARestart is the property: a consumed exemption is still consumed on the other side of a genesis round-trip.
func TestGenesis_MicroExemptionQuotaSurvivesARestart(t *testing.T) {
	ctx, k, _ := setupCoinRaw(t)
	ctx = ctx.WithBlockTime(time.Unix(1_700_000_000, 0))
	day := ctx.BlockTime().Unix() / 86400

	spender := sdk.AccAddress([]byte("micro-spender_______")).String()
	other := sdk.AccAddress([]byte("micro-other_________")).String()

	k.IncrMicroUsed(ctx, day, spender)
	k.IncrMicroUsed(ctx, day, other)
	k.IncrMicroUsed(ctx, day, other)
	require.Equal(t, uint64(1), k.GetMicroUsed(ctx, day, spender))
	require.Equal(t, uint64(2), k.GetMicroUsed(ctx, day, other))

	exported := k.ExportGenesis(ctx)
	require.NoError(t, exported.Validate())

	ctx2, k2, _ := setupCoinRaw(t)
	ctx2 = ctx2.WithBlockTime(ctx.BlockTime())
	require.NotPanics(t, func() { k2.InitGenesis(ctx2, *exported) })

	require.Equal(t, uint64(1), k2.GetMicroUsed(ctx2, day, spender),
		"a consumed daily exemption must not be handed back by a restart")
	require.Equal(t, uint64(2), k2.GetMicroUsed(ctx2, day, other),
		"the exemption counter must round-trip exactly, not be clamped or cleared")
}

// A genesis may not name store keys outside the one keyspace the raw entries exist to carry.
func TestGenesis_RejectsAStoreEntryOutsideTheQuotaKeyspace(t *testing.T) {
	ctx, k, _ := setupCoinRaw(t)

	for _, foreign := range [][]byte{types.ParamsKey, types.CoinAgePrefix} {
		gs := types.DefaultGenesis()
		gs.Params = k.GetParams(ctx)
		gs.StoreEntries = []types.StoreEntry{{
			Key:   append(append([]byte(nil), foreign...), []byte("x")...),
			Value: sdk.Uint64ToBigEndian(1),
		}}
		require.Error(t, gs.Validate(), "prefix %X must be refused in store_entries", foreign)
		require.Panics(t, func() { k.InitGenesis(ctx, *gs) })
	}
}

// The value is a big-endian counter the fee path reads back with BigEndianToUint64.
func TestGenesis_RejectsAMalformedQuotaValue(t *testing.T) {
	ctx, k, _ := setupCoinRaw(t)

	gs := types.DefaultGenesis()
	gs.Params = k.GetParams(ctx)
	gs.StoreEntries = []types.StoreEntry{{
		Key:   types.MicroQuotaKey(19_000, sdk.AccAddress([]byte("micro-spender_______")).String()),
		Value: []byte{0x01},
	}}
	require.Error(t, gs.Validate(), "a micro-quota counter that is not 8 bytes must be refused")
	require.Panics(t, func() { k.InitGenesis(ctx, *gs) })
}

// A counter seeded at 0xFF×8 (math.MaxUint64) is one IncrMicroUsed away from wrapping back to zero, which would silently hand the address a fresh exemption.
func TestGenesis_RejectsASaturatedQuotaCounter(t *testing.T) {
	ctx, k, _ := setupCoinRaw(t)

	gs := types.DefaultGenesis()
	gs.Params = k.GetParams(ctx)
	gs.StoreEntries = []types.StoreEntry{{
		Key:   types.MicroQuotaKey(19_000, sdk.AccAddress([]byte("micro-spender_______")).String()),
		Value: sdk.Uint64ToBigEndian(^uint64(0)), // MaxUint64: one increment from wrapping to zero
	}}
	require.Error(t, gs.Validate(), "a saturated micro-quota counter must be refused")
	require.Panics(t, func() { k.InitGenesis(ctx, *gs) })

	gs.StoreEntries[0].Value = sdk.Uint64ToBigEndian(^uint64(0) - 1)
	require.NoError(t, gs.Validate(), "a non-saturated counter must still be accepted")
}
