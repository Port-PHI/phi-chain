// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"testing"

	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/internal/storeprefix/prefixtest"
	"github.com/Port-PHI/phi-chain/x/governance/types"
)

// TestGenesis_RoundTripsEveryDeclaredStorePrefix seeds a record under every declared prefix through the real keeper writers and asserts one export→import cycle reproduces the module's whole keyspace.
func TestGenesis_RoundTripsEveryDeclaredStorePrefix(t *testing.T) {
	ctx, k := genesisSetup(t)
	const proposalID = uint64(42)

	k.SetProposalEligibility(ctx, proposalID, FrozenEligibility{
		Denominator: 9, Cutoff: 1_700_000_000, FrozenAt: 1_700_500_000,
	})
	require.True(t, k.RecordVote(ctx, proposalID, []byte("voter-aaaaaaaaaaaaaa"), int32(v1.OptionYes), true))
	require.True(t, k.RecordVote(ctx, proposalID, []byte("voter-bbbbbbbbbbbbbb"), int32(v1.OptionNo), true))
	require.False(t, k.RecordVote(ctx, proposalID, []byte("voter-cccccccccccccc"), int32(v1.OptionYes), false))
	k.EnqueueForPruning(ctx, proposalID+1)

	before := prefixtest.Dump(ctx, k.storeKey)
	prefixtest.RequireSeeded(t, before, types.AllStorePrefixes())

	exported := k.ExportGenesis(ctx)
	require.NoError(t, exported.Validate())

	ctx2, k2 := genesisSetup(t)
	require.NotPanics(t, func() { k2.InitGenesis(ctx2, *exported) })

	prefixtest.RequireRoundTrip(t, types.AllStorePrefixes(), before, prefixtest.Dump(ctx2, k2.storeKey))
}
