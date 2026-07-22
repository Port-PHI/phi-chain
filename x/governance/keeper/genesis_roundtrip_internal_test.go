// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/governance/types"
)

func genesisSetup(t *testing.T) (sdk.Context, Keeper) {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_gov_gen"))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	k := NewKeeper(cdc, key, sdk.AccAddress([]byte("gov_authority_______")).String())
	require.NoError(t, k.SetParams(testCtx.Ctx, types.DefaultParams()))
	return testCtx.Ctx, k
}

// TestGovGenesis_InFlightProposalSurvivesTheRoundTrip is the comprehensive case: a proposal mid-vote, with a frozen basis and several ballots, must come back byte-identical.
func TestGovGenesis_InFlightProposalSurvivesTheRoundTrip(t *testing.T) {
	ctx, k := genesisSetup(t)

	const proposalID = uint64(11)
	basis := FrozenEligibility{Denominator: 25, Cutoff: 1_700_000_000, FrozenAt: 1_700_500_000}
	k.SetProposalEligibility(ctx, proposalID, basis)

	voters := []struct {
		addr   []byte
		option v1.VoteOption
	}{
		{[]byte("voter-aaaaaaaaaaaaaa"), v1.OptionYes},
		{[]byte("voter-bbbbbbbbbbbbbb"), v1.OptionYes},
		{[]byte("voter-cccccccccccccc"), v1.OptionNo},
		{[]byte("voter-dddddddddddddd"), v1.OptionNoWithVeto},
		{[]byte("voter-eeeeeeeeeeeeee"), v1.OptionAbstain},
	}
	for _, v := range voters {
		require.True(t, k.RecordVote(ctx, proposalID, v.addr, int32(v.option), true))
	}
	require.False(t, k.RecordVote(ctx, proposalID, []byte("voter-ffffffffffffff"), int32(v1.OptionYes), false))
	k.EnqueueForPruning(ctx, proposalID+1)

	tallyBefore := k.GetRunningTally(ctx, proposalID)
	require.Equal(t, uint64(5), tallyBefore.Turnout, "precondition: the ballots were counted")

	exported := k.ExportGenesis(ctx)
	require.NoError(t, exported.Validate())

	importCtx, importK := genesisSetup(t)
	require.NotPanics(t, func() { importK.InitGenesis(importCtx, *exported) })

	require.Equal(t, tallyBefore, importK.GetRunningTally(importCtx, proposalID))

	gotBasis, ok := importK.GetProposalEligibility(importCtx, proposalID)
	require.True(t, ok, "the basis the ballots were counted against must survive")
	require.Equal(t, basis, gotBasis)

	for _, v := range voters {
		marker, found := importK.GetCountedVote(importCtx, proposalID, v.addr)
		require.True(t, found, "voter %s lost their contribution record", v.addr)
		require.Equal(t, byte(v.option), marker)
	}
	marker, found := importK.GetCountedVote(importCtx, proposalID, []byte("voter-ffffffffffffff"))
	require.True(t, found, "a refused ballot's marker must survive, or the refusal is re-litigated")
	require.Equal(t, types.IneligibleVoteMarker, marker)

	require.True(t, importK.IsQueuedForPruning(importCtx, proposalID+1),
		"the pruning queue must survive, or records are orphaned forever")

	require.Equal(t, exported, importK.ExportGenesis(importCtx))
}

// The raw keys are opaque, so genesis must refuse one that escapes the exported keyspace — otherwise a crafted genesis could name the params record and have it installed verbatim.
func TestGovGenesis_RejectsKeysOutsideTheExportedKeyspace(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  []byte
	}{
		{"the params record", types.ParamsKey},
		{"an unknown prefix", []byte{0x7F, 0x01}},
		{"a bare prefix with no record identity", types.PrunePrefix},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gs := types.GenesisState{
				Params:       types.DefaultParams(),
				StoreEntries: []types.StoreEntry{{Key: tc.key, Value: []byte{1}}},
			}
			require.Error(t, gs.Validate(), "a key outside the exported keyspace must not import")

			ctx, k := genesisSetup(t)
			require.Panics(t, func() { k.InitGenesis(ctx, gs) },
				"the same check must hold at the write, not only in Validate")
		})
	}
}
