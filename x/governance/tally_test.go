// SPDX-License-Identifier: Apache-2.0

package governance_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/governance"
	"github.com/Port-PHI/phi-chain/x/governance/types"
)

type fakeIDSource struct {
	minAge        time.Duration
	eligible      map[string]bool
	totalEligible uint64
}

func (f fakeIDSource) MinIdentityAge(sdk.Context) time.Duration { return f.minAge }
func (f fakeIDSource) IsEligibleControllerSince(ctx sdk.Context, controller string, t time.Time, minAge time.Duration, _ time.Time) bool {
	return f.IsEligibleControllerAt(ctx, controller, t, minAge)
}

func (f fakeIDSource) CountEligibleControllersAt(sdk.Context, time.Time, time.Duration) uint64 {
	return f.totalEligible
}
func (f fakeIDSource) IsEligibleControllerAt(_ sdk.Context, c string, _ time.Time, _ time.Duration) bool {
	return f.eligible[c]
}
func (f fakeIDSource) EligibleControllerTotal(sdk.Context) uint64 { return f.totalEligible }

type fakeRoutes struct{ table map[string]types.VoteRoute }

func (f fakeRoutes) VoteRouteTable(sdk.Context) map[string]types.VoteRoute { return f.table }

func defaultRoutes() fakeRoutes { return fakeRoutes{table: types.DefaultParams().RouteTable()} }

func TestClassifyByMsgType(t *testing.T) {
	ctx := sdk.Context{}

	technical := []sdk.Msg{&stakingtypes.MsgUpdateParams{}}
	require.Equal(t, governance.RouteTechnical, governance.ClassifyByMsgType(ctx, defaultRoutes(), technical))

	public := []sdk.Msg{&banktypes.MsgSend{}}
	require.Equal(t, governance.RoutePublic, governance.ClassifyByMsgType(ctx, defaultRoutes(), public))
}

func TestTallyOneHumanOneVote_WeightOnePerEligibleDID(t *testing.T) {
	src := fakeIDSource{
		minAge:        7 * 24 * time.Hour,
		totalEligible: 5,
		eligible: map[string]bool{
			"alice": true, "bob": true, "carol": true, "dave": true, "erin": true,
			"mallory": false, // below min_identity_age / inactive
		},
	}
	votes := []governance.VoteRecord{
		{Voter: "alice", Option: govv1.OptionYes},
		{Voter: "bob", Option: govv1.OptionYes},
		{Voter: "carol", Option: govv1.OptionNo},
		{Voter: "alice", Option: govv1.OptionNo},    // duplicate vote — must not be counted again
		{Voter: "mallory", Option: govv1.OptionYes}, // not eligible — must not be counted
	}

	res := governance.TallyOneHumanOneVote(sdk.Context{}, src, time.Unix(1000, 0), votes)
	require.Equal(t, uint64(5), res.TotalEligible)
	require.Equal(t, uint64(2), res.Yes, "alice (first vote) + bob")
	require.Equal(t, uint64(1), res.No, "carol")
	require.Equal(t, uint64(0), res.NoWithVeto)
	require.Equal(t, uint64(0), res.Abstain)
}

func TestTallyOneHumanOneVote_RejectsUnderMinAge(t *testing.T) {
	src := fakeIDSource{totalEligible: 1, eligible: map[string]bool{"new": false}}
	res := governance.TallyOneHumanOneVote(sdk.Context{}, src, time.Unix(1000, 0),
		[]governance.VoteRecord{{Voter: "new", Option: govv1.OptionYes}})
	require.Equal(t, uint64(0), res.Yes, "a DID below min_identity_age must not have its vote counted")
}

func TestOneHumanTally_Passes(t *testing.T) {
	quorum := math.LegacyNewDecWithPrec(334, 3)  // 33.4%
	threshold := math.LegacyNewDecWithPrec(5, 1) // 50%
	veto := math.LegacyNewDecWithPrec(334, 3)    // 33.4%

	pass := governance.OneHumanTally{Yes: 6, No: 2, Abstain: 2, TotalEligible: 10}
	require.True(t, pass.Passes(quorum, threshold, veto))

	noQuorum := governance.OneHumanTally{Yes: 2, TotalEligible: 10}
	require.False(t, noQuorum.Passes(quorum, threshold, veto))

	vetoed := governance.OneHumanTally{Yes: 6, NoWithVeto: 4, TotalEligible: 10}
	require.False(t, vetoed.Passes(quorum, threshold, veto))
}
