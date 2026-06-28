// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"testing"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/app"
)

// withBlockMaxGas returns a deep copy of the default consensus params with the given block MaxGas
// (the Block field is a shared pointer, so it is cloned to avoid mutating the global default).
func withBlockMaxGas(maxGas int64) cmtproto.ConsensusParams {
	cp := *simtestutil.DefaultConsensusParams
	block := *cp.Block
	block.MaxGas = maxGas
	cp.Block = &block
	return cp
}

// The InitChainer (via EnforceFiniteBlockMaxGas) must cap an unlimited (CometBFT default -1)
// block MaxGas to a finite value, so a maximally expensive single-message tx (gas > MaxGas) is
// rejected by the block gas meter. This capped value is exactly what the InitChainer assigns to
// res.ConsensusParams (res.ConsensusParams = &app.GetConsensusParams(ctx)) so CometBFT adopts the cap
// from the first block, not just the app-side store.
func TestInitChainer_BoundsUnlimitedBlockMaxGas(t *testing.T) {
	a := newTestApp(t)
	ctx := a.NewUncachedContext(false, cmtproto.Header{})
	require.NoError(t, a.StoreConsensusParams(ctx, withBlockMaxGas(-1)))

	require.NoError(t, a.EnforceFiniteBlockMaxGas(ctx))

	got := a.GetConsensusParams(ctx)
	require.Equal(t, app.DefaultBlockMaxGas, got.Block.MaxGas, "unlimited block max gas must be capped to a finite default")
	require.Positive(t, got.Block.MaxGas)
}

// An explicitly-configured finite block MaxGas is respected (not overwritten).
func TestInitChainer_RespectsExplicitFiniteBlockMaxGas(t *testing.T) {
	a := newTestApp(t)
	ctx := a.NewUncachedContext(false, cmtproto.Header{})
	require.NoError(t, a.StoreConsensusParams(ctx, withBlockMaxGas(7_000_000)))

	require.NoError(t, a.EnforceFiniteBlockMaxGas(ctx))

	require.Equal(t, int64(7_000_000), a.GetConsensusParams(ctx).Block.MaxGas, "an explicit finite max gas is respected")
}
