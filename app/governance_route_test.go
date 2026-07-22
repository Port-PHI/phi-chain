// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"testing"
	"time"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/governance"
	governancetypes "github.com/Port-PHI/phi-chain/x/governance/types"
)

// The wiring is live: the app's custom tally reads the route from the GOVERNED table in the governance keeper, and a proposal that rewrites that table can actually be executed by governance (its Msg service is routed) — but only ever after being decided on the public path.
func TestGovernanceRouting_IsWiredToTheGovernedTable(t *testing.T) {
	a := newTestApp(t)
	ctx := a.NewUncachedContext(false, cmtproto.Header{Height: 1, Time: time.Unix(1_700_000_000, 0).UTC()})

	k := a.GovernanceKeeper
	require.Equal(t, governancetypes.DefaultParams().VoteRoutes, k.GetParams(ctx).VoteRoutes)

	require.Equal(t, governance.RouteTechnical,
		governance.ClassifyByMsgType(ctx, k, []sdk.Msg{&stakingtypes.MsgUpdateParams{}}))

	require.Equal(t, governance.RoutePublic,
		governance.ClassifyByMsgType(ctx, k, []sdk.Msg{&governancetypes.MsgUpdateParams{}}))

	require.Equal(t, governance.RoutePublic, governance.ClassifyByMsgType(ctx, k, []sdk.Msg{
		&stakingtypes.MsgUpdateParams{}, &governancetypes.MsgUpdateParams{},
	}))

	require.NotNil(t, a.MsgServiceRouter().Handler(&governancetypes.MsgUpdateParams{}),
		"the mapping-update message must be routable, or a passed public vote could not take effect")

	require.Equal(t, a.GovKeeper.GetAuthority(), k.GetAuthority())
}
