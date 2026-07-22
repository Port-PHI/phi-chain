// SPDX-License-Identifier: Apache-2.0

package governance

import (
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/governance/keeper"
	"github.com/Port-PHI/phi-chain/x/governance/types"
)

type ballot struct {
	voter    int
	option   v1.VoteOption
	eligible bool
}

type fakeIdentity struct {
	eligible    map[string]bool
	denominator uint64
	total       uint64
}

func (f fakeIdentity) IsEligibleControllerAt(_ sdk.Context, controller string, _ time.Time, _ time.Duration) bool {
	return f.eligible[controller]
}

// IsEligibleControllerSince adds the frozen-basis continuity test.
func (f fakeIdentity) IsEligibleControllerSince(ctx sdk.Context, controller string, t time.Time, minAge time.Duration, _ time.Time) bool {
	return f.IsEligibleControllerAt(ctx, controller, t, minAge)
}

func (f fakeIdentity) CountEligibleControllersAt(_ sdk.Context, _ time.Time, _ time.Duration) uint64 {
	return f.denominator
}

// EligibleControllerTotal is the O(1) counter the EndBlocker fallback reads.
func (f fakeIdentity) EligibleControllerTotal(_ sdk.Context) uint64 {
	if f.total != 0 {
		return f.total
	}
	return f.denominator
}
func (f fakeIdentity) MinIdentityAge(_ sdk.Context) time.Duration { return 0 }

func govSetup(t *testing.T) (sdk.Context, keeper.Keeper) {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_gov_eb"))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	k := keeper.NewKeeper(cdc, key, sdk.AccAddress([]byte("gov_authority_______")).String())
	require.NoError(t, k.SetParams(testCtx.Ctx, types.DefaultParams()))
	return testCtx.Ctx, k
}

func testValidators() map[string]v1.ValidatorGovInfo {
	return map[string]v1.ValidatorGovInfo{
		"v1": {BondedTokens: math.NewInt(600)},
		"v2": {BondedTokens: math.NewInt(400)},
	}
}

func testProposal(id uint64) v1.Proposal {
	start := time.Unix(1_000_000, 0)
	return v1.Proposal{Id: id, VotingStartTime: &start}
}

func castAll(ctx sdk.Context, k keeper.Keeper, proposalID uint64, ballots []ballot) {
	for _, b := range ballots {
		k.RecordVote(ctx, proposalID, []byte(fmt.Sprintf("voter-%08d", b.voter)), int32(b.option), b.eligible)
	}
}

func referenceTally(ballots []ballot, denominator uint64, validators map[string]v1.ValidatorGovInfo) (math.LegacyDec, map[v1.VoteOption]math.LegacyDec) {
	latest := map[int]v1.VoteOption{}
	counted := map[int]bool{}
	for _, b := range ballots {
		if counted[b.voter] {
			if _, ok := latest[b.voter]; ok {
				latest[b.voter] = b.option
			}
			continue
		}
		counted[b.voter] = true
		if b.eligible {
			latest[b.voter] = b.option
		}
	}
	counts := map[v1.VoteOption]uint64{}
	for _, opt := range latest {
		counts[opt]++
	}
	return scalePublicResults(counts, uint64(len(latest)), denominator, sumBonded(validators))
}

// The end-of-voting tally must produce what the walk-every-ballot implementation produced.
func TestPublicTally_MatchesReferenceCounting(t *testing.T) {
	scenarios := []struct {
		name        string
		ballots     []ballot
		denominator uint64
	}{
		{
			name:        "unanimous yes",
			ballots:     []ballot{{1, v1.OptionYes, true}, {2, v1.OptionYes, true}, {3, v1.OptionYes, true}},
			denominator: 10,
		},
		{
			name: "mixed options",
			ballots: []ballot{
				{1, v1.OptionYes, true}, {2, v1.OptionNo, true},
				{3, v1.OptionAbstain, true}, {4, v1.OptionNoWithVeto, true},
			},
			denominator: 8,
		},
		{
			name: "ineligible voters are excluded",
			ballots: []ballot{
				{1, v1.OptionYes, true}, {2, v1.OptionYes, false},
				{3, v1.OptionNo, false}, {4, v1.OptionNo, true},
			},
			denominator: 6,
		},
		{
			name: "changed ballots keep one contribution each",
			ballots: []ballot{
				{1, v1.OptionYes, true}, {1, v1.OptionNo, true},
				{2, v1.OptionNo, true}, {2, v1.OptionYes, true}, {2, v1.OptionAbstain, true},
			},
			denominator: 5,
		},
		{
			name:        "turnout of zero",
			ballots:     nil,
			denominator: 9,
		},
		{
			name:        "every voter ineligible",
			ballots:     []ballot{{1, v1.OptionYes, false}, {2, v1.OptionNo, false}},
			denominator: 4,
		},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			ctx, k := govSetup(t)
			validators := testValidators()
			proposal := testProposal(1)

			castAll(ctx, k, proposal.Id, sc.ballots)
			k.SetProposalEligibility(ctx, proposal.Id, keeper.FrozenEligibility{Denominator: sc.denominator})

			gotPower, gotResults, err := tallyPublicLive(ctx, k, proposal, fakeIdentity{}, validators)
			require.NoError(t, err)

			wantPower, wantResults := referenceTally(sc.ballots, sc.denominator, validators)
			require.Equal(t, wantPower.String(), gotPower.String(), "total voting power")
			for _, opt := range []v1.VoteOption{v1.OptionYes, v1.OptionAbstain, v1.OptionNo, v1.OptionNoWithVeto} {
				require.Equal(t, wantResults[opt].String(), gotResults[opt].String(), "option %s", opt)
			}
		})
	}
}

// No disenfranchisement: every eligible voter is counted, at any scale.
func TestPublicTally_CountsEveryEligibleVoter(t *testing.T) {
	for _, voters := range []int{1, 100, 5_000} {
		t.Run(fmt.Sprintf("%d voters", voters), func(t *testing.T) {
			ctx, k := govSetup(t)
			proposal := testProposal(1)

			ballots := make([]ballot, 0, voters)
			for i := 0; i < voters; i++ {
				ballots = append(ballots, ballot{voter: i, option: v1.OptionYes, eligible: true})
			}
			castAll(ctx, k, proposal.Id, ballots)
			k.SetProposalEligibility(ctx, proposal.Id,
				keeper.FrozenEligibility{Denominator: uint64(voters)})

			require.Equal(t, uint64(voters), k.GetRunningTally(ctx, proposal.Id).Turnout,
				"every eligible voter must be counted")

			power, results, err := tallyPublicLive(ctx, k, proposal, fakeIdentity{}, testValidators())
			require.NoError(t, err)
			require.Equal(t, math.LegacyNewDec(1000).String(), power.String())
			require.Equal(t, math.LegacyNewDec(1000).String(), results[v1.OptionYes].String())
		})
	}
}

// The property this whole change exists for: the end-of-voting tally costs the same whether ten people voted or ten thousand.
func TestPublicTally_GasIsFlatInVoteCount(t *testing.T) {
	run := func(voters int) (uint64, math.LegacyDec) {
		ctx, k := govSetup(t)
		proposal := testProposal(1)

		ballots := make([]ballot, 0, voters)
		for i := 0; i < voters; i++ {
			ballots = append(ballots, ballot{voter: i, option: v1.OptionYes, eligible: true})
		}
		castAll(ctx, k, proposal.Id, ballots)
		k.SetProposalEligibility(ctx, proposal.Id, keeper.FrozenEligibility{Denominator: 20_000})

		ctx = ctx.WithGasMeter(storetypes.NewGasMeter(1_000_000_000))
		before := ctx.GasMeter().GasConsumed()
		power, _, err := tallyPublicLive(ctx, k, proposal, fakeIdentity{}, testValidators())
		require.NoError(t, err)
		return ctx.GasMeter().GasConsumed() - before, power
	}

	smallGas, smallPower := run(10)
	largeGas, largePower := run(10_000)

	t.Logf("end-of-voting tally gas: 10 votes = %d, 10000 votes = %d", smallGas, largeGas)

	require.NotEqual(t, smallPower.String(), largePower.String(), "the two runs must differ in result")
	require.Equal(t, smallGas, largeGas,
		"end-of-voting tally gas must not grow with the number of votes cast (10 vs 10000)")
}

// Deletion of stale vote records cannot change a result: a proposal is queued only once its tally is final.
func TestPublicTally_ResultIsUnaffectedByPruning(t *testing.T) {
	ctx, k := govSetup(t)
	proposal := testProposal(1)

	ballots := []ballot{
		{1, v1.OptionYes, true}, {2, v1.OptionYes, true},
		{3, v1.OptionNo, true}, {4, v1.OptionAbstain, true},
	}
	castAll(ctx, k, proposal.Id, ballots)
	k.SetProposalEligibility(ctx, proposal.Id, keeper.FrozenEligibility{Denominator: 8})

	beforePower, beforeResults, err := tallyPublicLive(ctx, k, proposal, fakeIdentity{}, testValidators())
	require.NoError(t, err)

	k.EnqueueForPruning(ctx, proposal.Id)
	steps := 0
	for k.IsQueuedForPruning(ctx, proposal.Id) {
		k.PruneStep(ctx, 2) // deliberately tiny budget: several blocks' worth
		steps++
		require.Less(t, steps, 50, "pruning must make progress")
	}
	require.Greater(t, steps, 1, "a tiny budget must spread the work over more than one block")

	wantPower, wantResults := referenceTally(ballots, 8, testValidators())
	require.Equal(t, wantPower.String(), beforePower.String())
	require.Equal(t, wantResults[v1.OptionYes].String(), beforeResults[v1.OptionYes].String())

	require.Equal(t, uint64(0), k.GetRunningTally(ctx, proposal.Id).Turnout)
	for i := 1; i <= 4; i++ {
		_, found := k.GetCountedVote(ctx, proposal.Id, []byte(fmt.Sprintf("voter-%08d", i)))
		require.False(t, found)
	}
}

// A ballot cast while the voter was eligible stays counted even if that voter's identity is revoked before voting ends.
func TestPublicTally_FrozenEligibilityKeepsAValidBallot(t *testing.T) {
	ctx, k := govSetup(t)
	proposal := testProposal(1)

	castAll(ctx, k, proposal.Id, []ballot{{1, v1.OptionYes, true}, {2, v1.OptionYes, true}})
	k.SetProposalEligibility(ctx, proposal.Id, keeper.FrozenEligibility{Denominator: 4})

	revoked := fakeIdentity{eligible: map[string]bool{}, denominator: 4}

	power, results, err := tallyPublicLive(ctx, k, proposal, revoked, testValidators())
	require.NoError(t, err)

	require.Equal(t, uint64(2), k.GetRunningTally(ctx, proposal.Id).Turnout,
		"ballots cast while eligible must remain counted")
	require.Equal(t, math.LegacyNewDec(500).String(), power.String())
	require.Equal(t, math.LegacyNewDec(500).String(), results[v1.OptionYes].String())
}

// A proposal that reached a tally without a frozen basis must fall back to the current eligible set.
func TestPublicTally_FallsBackWhenNoFrozenBasis(t *testing.T) {
	ctx, k := govSetup(t)
	proposal := testProposal(1)

	castAll(ctx, k, proposal.Id, []ballot{{1, v1.OptionYes, true}, {2, v1.OptionYes, true}})

	power, _, err := tallyPublicLive(ctx, k, proposal, fakeIdentity{denominator: 4}, testValidators())
	require.NoError(t, err)
	require.Equal(t, math.LegacyNewDec(500).String(), power.String(),
		"a missing basis must fall back to the live count, not to zero")
}
