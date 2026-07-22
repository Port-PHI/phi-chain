// SPDX-License-Identifier: Apache-2.0

package governance

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/governance/keeper"
)

func runTally(ctx sdk.Context, k keeper.Keeper, proposal *v1.Proposal, passes bool) {
	if prunableAtTally(*proposal) {
		k.EnqueueForPruning(ctx, proposal.Id)
	}

	switch {
	case passes:
		proposal.Status = v1.StatusPassed
	case proposal.Expedited:
		proposal.Expedited = false
		proposal.Status = v1.StatusVotingPeriod
	default:
		proposal.Status = v1.StatusRejected
	}

	if prunableAfterVoting(proposal.Status) {
		k.EnqueueForPruning(ctx, proposal.Id)
	}
}

// TestPrune_FailedExpeditedProposalKeepsItsBasisAndBallots is the comprehensive case: a single voter re-casting across an extended voting period must not inflate turnout, because nothing of theirs was deleted in between.
func TestPrune_FailedExpeditedProposalKeepsItsBasisAndBallots(t *testing.T) {
	ctx, k := govSetup(t)
	now := int64(5_000_000)
	ctx = ctx.WithBlockTime(time.Unix(now, 0))

	reg := newModelRegistry(&now)
	voter := voterAddr(1)
	for i := 0; i < 25; i++ {
		reg.register(voterAddr(i).String(), now-10_000)
	}

	hooks := NewVoteHooks(k, nil, reg, publicRoutes{}, noValidators{})
	proposal := proposalAt(11, now)
	proposal.Expedited = true

	basis := hooks.freezeEligibilityOnce(ctx, proposal)
	require.Equal(t, uint64(25), basis.Denominator)

	require.NoError(t, hooks.recordBallot(ctx, proposal, voter.String(), voter.Bytes(), single(v1.OptionYes)))
	require.Equal(t, uint64(1), k.GetRunningTally(ctx, proposal.Id).Turnout)

	runTally(ctx, k, &proposal, false)
	require.Equal(t, v1.StatusVotingPeriod, proposal.Status, "the proposal must still be open")
	require.False(t, k.IsQueuedForPruning(ctx, proposal.Id),
		"a proposal still in its voting period must never be queued for pruning")

	k.PruneStep(ctx, PruneBudgetPerBlock)
	stillFrozen, ok := k.GetProposalEligibility(ctx, proposal.Id)
	require.True(t, ok, "the frozen basis must survive the extended voting period")
	require.Equal(t, basis, stillFrozen, "and it must be the SAME basis, not a re-frozen one")

	for i := 0; i < 25; i++ {
		now += 60
		ctx = ctx.WithBlockTime(time.Unix(now, 0)).WithBlockHeight(ctx.BlockHeight() + 1)
		require.NoError(t, hooks.recordBallot(ctx, proposal, voter.String(), voter.Bytes(),
			single(v1.OptionYes)))
		k.PruneStep(ctx, PruneBudgetPerBlock)

		running := k.GetRunningTally(ctx, proposal.Id)
		require.Equal(t, uint64(1), running.Turnout,
			"re-cast %d: one voter is one head, however many times they vote", i+1)
		require.LessOrEqual(t, running.Turnout, basis.Denominator)
	}

	require.Equal(t, uint64(1), k.GetRunningTally(ctx, proposal.Id).Counts[int32(v1.OptionYes)])

	msg, broken := TurnoutWithinFrozenBasisInvariant(k)(ctx)
	require.False(t, broken, msg)

	runTally(ctx, k, &proposal, true)
	require.Equal(t, v1.StatusPassed, proposal.Status)
	require.True(t, k.IsQueuedForPruning(ctx, proposal.Id),
		"a finished proposal must be queued for pruning")
}

// A regular proposal is finished by its first tally, so it is queued straight away — the fix must not leave ordinary proposals' records behind forever.
func TestPrune_RegularProposalsAreQueuedAsSoonAsTheyAreTallied(t *testing.T) {
	for _, tc := range []struct {
		name   string
		passes bool
		status v1.ProposalStatus
	}{
		{"passed", true, v1.StatusPassed},
		{"rejected", false, v1.StatusRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, k := govSetup(t)
			proposal := testProposal(3)
			require.False(t, proposal.Expedited)

			runTally(ctx, k, &proposal, tc.passes)
			require.Equal(t, tc.status, proposal.Status)
			require.True(t, k.IsQueuedForPruning(ctx, proposal.Id))
		})
	}
}

// An expedited proposal that PASSES is terminal on its first tally, and must be queued too — by the post-voting hook, since the tally itself could not yet know.
func TestPrune_PassedExpeditedProposalIsQueued(t *testing.T) {
	ctx, k := govSetup(t)
	proposal := testProposal(4)
	proposal.Expedited = true

	runTally(ctx, k, &proposal, true)
	require.Equal(t, v1.StatusPassed, proposal.Status)
	require.True(t, k.IsQueuedForPruning(ctx, proposal.Id),
		"an expedited proposal that passes is finished and must be queued")
}

// The two decisions in isolation, so a future edit to either cannot quietly widen it.
func TestPrune_TheDecisionsThemselves(t *testing.T) {
	require.True(t, prunableAtTally(v1.Proposal{}), "a regular proposal's tally ends it")
	require.False(t, prunableAtTally(v1.Proposal{Expedited: true}),
		"an expedited tally may convert rather than end the proposal")

	require.False(t, prunableAfterVoting(v1.StatusVotingPeriod), "still open")
	for _, s := range []v1.ProposalStatus{
		v1.StatusPassed, v1.StatusRejected, v1.StatusFailed, v1.StatusDepositPeriod,
	} {
		require.True(t, prunableAfterVoting(s), "%s is not a voting period", s)
	}
}
