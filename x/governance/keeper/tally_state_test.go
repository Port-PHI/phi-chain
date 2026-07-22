// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/governance/keeper"
	"github.com/Port-PHI/phi-chain/x/governance/types"
)

const (
	optYes     = 1
	optAbstain = 2
	optNo      = 3
	optVeto    = 4
)

func voterAddr(i int) []byte { return []byte(fmt.Sprintf("voter-%013d", i)) }

// A counted ballot raises exactly one option and the turnout, once.
func TestRecordVote_CountsEligibleBallot(t *testing.T) {
	ctx, k, _, _ := setup(t)

	require.True(t, k.RecordVote(ctx, 1, voterAddr(1), optYes, true))

	tally := k.GetRunningTally(ctx, 1)
	require.Equal(t, uint64(1), tally.Turnout)
	require.Equal(t, uint64(1), tally.Counts[optYes])
	require.Equal(t, uint64(0), tally.Counts[optNo])
}

// Changing a vote must retract the previous option and apply the new one, leaving the counts exactly where a full recount would put them — and never inflating turnout, since it is the same human.
func TestRecordVote_ChangedBallotAppliesExactDelta(t *testing.T) {
	ctx, k, _, _ := setup(t)

	k.RecordVote(ctx, 1, voterAddr(1), optYes, true)
	k.RecordVote(ctx, 1, voterAddr(1), optNo, true)

	tally := k.GetRunningTally(ctx, 1)
	require.Equal(t, uint64(0), tally.Counts[optYes], "the retracted option must be decremented")
	require.Equal(t, uint64(1), tally.Counts[optNo])
	require.Equal(t, uint64(1), tally.Turnout, "changing a vote is not a second voter")
}

// Re-submitting the same option must be a no-op, not a second count.
func TestRecordVote_RepeatedIdenticalBallotIsIdempotent(t *testing.T) {
	ctx, k, _, _ := setup(t)

	for i := 0; i < 5; i++ {
		k.RecordVote(ctx, 1, voterAddr(1), optYes, true)
	}

	tally := k.GetRunningTally(ctx, 1)
	require.Equal(t, uint64(1), tally.Counts[optYes])
	require.Equal(t, uint64(1), tally.Turnout)
}

// One vote per controller: repeated ballots from the same voter never accumulate.
func TestRecordVote_OneContributionPerVoter(t *testing.T) {
	ctx, k, _, _ := setup(t)

	options := []int32{optYes, optNo, optAbstain, optVeto, optYes}
	for _, opt := range options {
		k.RecordVote(ctx, 1, voterAddr(1), opt, true)
	}

	tally := k.GetRunningTally(ctx, 1)
	require.Equal(t, uint64(1), tally.Turnout)
	var total uint64
	for _, n := range tally.Counts {
		total += n
	}
	require.Equal(t, uint64(1), total, "one voter must contribute exactly one ballot in total")
}

// An ineligible voter is marked and never counted; repeat attempts stay cheap and are not re-judged.
func TestRecordVote_IneligibleVoterIsMarkedAndNeverCounted(t *testing.T) {
	ctx, k, _, _ := setup(t)

	require.False(t, k.RecordVote(ctx, 1, voterAddr(1), optYes, false))

	marker, found := k.GetCountedVote(ctx, 1, voterAddr(1))
	require.True(t, found)
	require.Equal(t, types.IneligibleVoteMarker, marker)

	tally := k.GetRunningTally(ctx, 1)
	require.Equal(t, uint64(0), tally.Turnout)
	require.Equal(t, uint64(0), tally.Counts[optYes])
}

// Eligibility is decided once, at the first ballot.
func TestRecordVote_IneligibilityIsFrozenForTheProposal(t *testing.T) {
	ctx, k, _, _ := setup(t)

	require.False(t, k.RecordVote(ctx, 1, voterAddr(1), optYes, false))
	require.False(t, k.RecordVote(ctx, 1, voterAddr(1), optYes, true),
		"a voter judged ineligible must not be re-evaluated within the same proposal")

	require.Equal(t, uint64(0), k.GetRunningTally(ctx, 1).Turnout)
}

// The mirror of the rule above: a ballot counted while the voter was eligible stays counted.
func TestRecordVote_CountedBallotSurvivesLaterIneligibility(t *testing.T) {
	ctx, k, _, _ := setup(t)

	k.RecordVote(ctx, 1, voterAddr(1), optYes, true)
	k.RecordVote(ctx, 1, voterAddr(1), optNo, false)

	tally := k.GetRunningTally(ctx, 1)
	require.Equal(t, uint64(1), tally.Turnout, "an already-counted voter stays counted")
	require.Equal(t, uint64(1), tally.Counts[optNo])
}

// Proposals must not bleed into each other.
func TestRecordVote_ProposalsAreIndependent(t *testing.T) {
	ctx, k, _, _ := setup(t)

	k.RecordVote(ctx, 1, voterAddr(1), optYes, true)
	k.RecordVote(ctx, 2, voterAddr(1), optNo, true)

	require.Equal(t, uint64(1), k.GetRunningTally(ctx, 1).Counts[optYes])
	require.Equal(t, uint64(0), k.GetRunningTally(ctx, 1).Counts[optNo])
	require.Equal(t, uint64(1), k.GetRunningTally(ctx, 2).Counts[optNo])
	require.Equal(t, uint64(0), k.GetRunningTally(ctx, 2).Counts[optYes])
}

// The frozen basis must round-trip exactly, including a negative cutoff (a proposal opened close to the epoch, or a long minimum identity age).
func TestProposalEligibility_RoundTripsIncludingNegativeCutoff(t *testing.T) {
	ctx, k, _, _ := setup(t)

	_, ok := k.GetProposalEligibility(ctx, 1)
	require.False(t, ok, "no basis exists until one is frozen")

	for _, want := range []keeper.FrozenEligibility{
		{Denominator: 1000, Cutoff: 1_700_000_000},
		{Denominator: 0, Cutoff: -5},
	} {
		k.SetProposalEligibility(ctx, 7, want)
		got, ok := k.GetProposalEligibility(ctx, 7)
		require.True(t, ok)
		require.Equal(t, want, got)
	}
}

// Pruning must respect its budget, make progress every block, and eventually remove everything — records, aggregates and the queue entry alike.
func TestPruneStep_IsBudgetedAndEventuallyComplete(t *testing.T) {
	ctx, k, _, _ := setup(t)

	const voters = 250
	for i := 0; i < voters; i++ {
		k.RecordVote(ctx, 1, voterAddr(i), optYes, true)
	}
	k.SetProposalEligibility(ctx, 1, keeper.FrozenEligibility{Denominator: 500, Cutoff: 10})
	k.EnqueueForPruning(ctx, 1)

	const budget = 40
	steps := 0
	for k.IsQueuedForPruning(ctx, 1) {
		deleted := k.PruneStep(ctx, budget)
		require.LessOrEqual(t, deleted, budget, "a sweep must never exceed its budget")
		steps++
		require.Less(t, steps, 100, "pruning must make progress every block")
	}

	for i := 0; i < voters; i++ {
		_, found := k.GetCountedVote(ctx, 1, voterAddr(i))
		require.False(t, found, "voter %d record must be pruned", i)
	}
	tally := k.GetRunningTally(ctx, 1)
	require.Equal(t, uint64(0), tally.Turnout)
	require.Empty(t, tally.Counts)
	_, ok := k.GetProposalEligibility(ctx, 1)
	require.False(t, ok)
	require.False(t, k.IsQueuedForPruning(ctx, 1))
}

// Pruning one proposal must not touch another that is still open.
func TestPruneStep_LeavesOtherProposalsIntact(t *testing.T) {
	ctx, k, _, _ := setup(t)

	for i := 0; i < 20; i++ {
		k.RecordVote(ctx, 1, voterAddr(i), optYes, true)
		k.RecordVote(ctx, 2, voterAddr(i), optNo, true)
	}
	k.EnqueueForPruning(ctx, 1)

	for k.IsQueuedForPruning(ctx, 1) {
		k.PruneStep(ctx, 100)
	}

	require.Equal(t, uint64(0), k.GetRunningTally(ctx, 1).Turnout)
	open := k.GetRunningTally(ctx, 2)
	require.Equal(t, uint64(20), open.Turnout, "an unqueued proposal must be untouched")
	require.Equal(t, uint64(20), open.Counts[optNo])
}

// A sweep with nothing queued must be a no-op rather than an error or a spin.
func TestPruneStep_NoopWhenQueueEmpty(t *testing.T) {
	ctx, k, _, _ := setup(t)
	require.Equal(t, 0, k.PruneStep(ctx, 100))
	require.Equal(t, 0, k.PruneStep(ctx, 0))
}
