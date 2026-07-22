// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/governance"
	"github.com/Port-PHI/phi-chain/x/governance/keeper"
	"github.com/Port-PHI/phi-chain/x/governance/types"
)

func setup(t *testing.T) (sdk.Context, keeper.Keeper, types.MsgServer, string) {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_gov"))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	authority := sdk.AccAddress([]byte("gov_authority_______")).String()
	k := keeper.NewKeeper(cdc, key, authority)
	require.NoError(t, k.SetParams(testCtx.Ctx, types.DefaultParams()))
	return testCtx.Ctx, k, keeper.NewMsgServerImpl(k), authority
}

// The route a proposal takes is read from the GOVERNED table in state — so a governance change to the table really does change how future proposals are decided.
func TestVoteRouteTable_IsGovernedState(t *testing.T) {
	ctx, k, msg, authority := setup(t)

	staking := []sdk.Msg{&stakingtypes.MsgUpdateParams{}}
	require.Equal(t, governance.RouteTechnical, governance.ClassifyByMsgType(ctx, k, staking))

	moved := types.DefaultParams()
	for i := range moved.VoteRoutes {
		if moved.VoteRoutes[i].MsgTypeUrl == sdk.MsgTypeURL(&stakingtypes.MsgUpdateParams{}) {
			moved.VoteRoutes[i].Route = types.VOTE_ROUTE_PUBLIC
		}
	}
	_, err := msg.UpdateParams(ctx, &types.MsgUpdateParams{Authority: authority, Params: moved})
	require.NoError(t, err)

	require.Equal(t, governance.RoutePublic, governance.ClassifyByMsgType(ctx, k, staking),
		"the mapping is on-chain state; changing it must change the route")
}

// ANTI-CAPTURE, through the live keeper: whatever the table in state says, a proposal that rewrites the table is classified PUBLIC.
func TestAntiCapture_MappingChangeIsPublicAgainstLiveState(t *testing.T) {
	ctx, k, _, _ := setup(t)

	mappingUpdate := []sdk.Msg{&types.MsgUpdateParams{}}
	require.Equal(t, governance.RoutePublic, governance.ClassifyByMsgType(ctx, k, mappingUpdate))

	poisoned := types.Params{VoteRoutes: []types.VoteRouteEntry{
		{MsgTypeUrl: types.MappingUpdateMsgTypeURL, Route: types.VOTE_ROUTE_TECHNICAL},
	}}
	require.Error(t, k.SetParams(ctx, poisoned),
		"state must never be able to hold a claim that a mapping change is technical")

	require.Equal(t, governance.RoutePublic,
		types.Classify(mappingUpdate, poisoned.RouteTable()),
		"the fixed rule wins over any table content, always")
}

// A mapping change is authority-gated like any other governed param.
func TestUpdateParams_AuthorityGated(t *testing.T) {
	ctx, k, msg, authority := setup(t)
	stranger := sdk.AccAddress([]byte("not_the_authority___")).String()

	_, err := msg.UpdateParams(ctx, &types.MsgUpdateParams{Authority: stranger, Params: types.Params{}})
	require.ErrorIs(t, err, govtypes.ErrInvalidSigner)
	require.Equal(t, types.DefaultParams().VoteRoutes, k.GetParams(ctx).VoteRoutes,
		"a rejected update must change nothing")

	_, err = msg.UpdateParams(ctx, &types.MsgUpdateParams{Authority: authority, Params: types.Params{}})
	require.NoError(t, err)
	require.Empty(t, k.GetParams(ctx).VoteRoutes)
	require.Equal(t, governance.RoutePublic,
		governance.ClassifyByMsgType(ctx, k, []sdk.Msg{&stakingtypes.MsgUpdateParams{}}),
		"an empty table routes everything to the public path, never to the validator path")
}

// Genesis round-trips the table through the keeper.
func TestGenesis_RoundTrip(t *testing.T) {
	ctx, k, _, _ := setup(t)

	gs := types.DefaultGenesis()
	k.InitGenesis(ctx, *gs)
	got := k.ExportGenesis(ctx)
	require.Equal(t, gs.Params, got.Params)
	require.Empty(t, got.StoreEntries)
	require.Equal(t, governance.RouteTechnical,
		governance.ClassifyByMsgType(ctx, k, []sdk.Msg{&stakingtypes.MsgUpdateParams{}}))
}
