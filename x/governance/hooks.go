// SPDX-License-Identifier: Apache-2.0

package governance

import (
	"context"
	"time"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"github.com/Port-PHI/phi-chain/x/governance/keeper"
	"github.com/Port-PHI/phi-chain/x/governance/types"
)

// VoteHooks accumulates the public-route tally at vote time via x/gov hooks.
type VoteHooks struct {
	k   keeper.Keeper
	gov *govkeeper.Keeper
	idk IdentitySource
	rs  RouteSource
	vs  ValidatorSource
}

var _ govtypes.GovHooks = VoteHooks{}

func NewVoteHooks(k keeper.Keeper, gov *govkeeper.Keeper, idk IdentitySource, rs RouteSource, vs ValidatorSource) VoteHooks {
	return VoteHooks{k: k, gov: gov, idk: idk, rs: rs, vs: vs}
}

// AfterProposalDeposit freezes the proposal's eligibility basis once, when it enters its voting period.
func (h VoteHooks) AfterProposalDeposit(ctx context.Context, proposalID uint64, _ sdk.AccAddress) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	proposal, err := h.gov.Proposals.Get(ctx, proposalID)
	if err != nil {
		return nil // not our concern: x/gov surfaces its own errors for a missing proposal
	}
	if proposal.Status != v1.StatusVotingPeriod {
		return nil // still in the deposit period; nothing to freeze yet
	}
	h.freezeEligibilityOnce(sdkCtx, proposal)
	return nil
}

// AfterProposalVote folds one cast ballot into the running tally.
func (h VoteHooks) AfterProposalVote(ctx context.Context, proposalID uint64, voterAddr sdk.AccAddress) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	proposal, err := h.gov.Proposals.Get(ctx, proposalID)
	if err != nil {
		return nil
	}
	vote, err := h.gov.Votes.Get(ctx, collections.Join(proposalID, sdk.AccAddress(voterAddr)))
	if err != nil {
		return nil // x/gov stores the vote before calling this hook; nothing to do if it is absent
	}

	return h.recordBallot(sdkCtx, proposal, vote.Voter, voterAddr.Bytes(), vote.Options)
}

func (h VoteHooks) recordBallot(ctx sdk.Context, proposal v1.Proposal, voter string, voterAddr []byte, options []*v1.WeightedVoteOption) error {
	// A proposal that never went through the deposit hook (or one activated by another path) still needs a basis; take it now so the first ballot is judged against a frozen rule like every other.
	basis := h.freezeEligibilityOnce(ctx, proposal)

	if h.routeOf(ctx, proposal) == RouteTechnical {
		// The technical tally reads each active validator's own ballot straight from x/gov and never consults the running tally, so nothing is accumulated here — only the ineligible are turned away, for the same reason as on the public route: a silently uncounted vote is worse than a refused one.
		if !h.vs.IsActiveValidatorOperator(ctx, voterAddr) {
			return errorsmod.Wrapf(types.ErrNotEligibleToVote,
				"%s is not an active validator (technical proposal %d)", voter, proposal.Id)
		}
		return nil
	}

	// Judged against the frozen basis in BOTH of its dimensions — the cutoff the denominator was filtered by, and the moment it was counted at — so the counted set cannot escape the set it is measured against.
	if !h.idk.IsEligibleControllerSince(ctx, voter, timeFromUnix(basis.Cutoff), 0, timeFromUnix(basis.FrozenAt)) {
		return errorsmod.Wrapf(types.ErrNotEligibleToVote,
			"%s (proposal %d, cutoff %d, frozen at %d)", voter, proposal.Id, basis.Cutoff, basis.FrozenAt)
	}
	h.k.RecordVote(ctx, proposal.Id, voterAddr, int32(dominantOption(options)), true)
	return nil
}

func (h VoteHooks) routeOf(ctx sdk.Context, proposal v1.Proposal) VoteRoute {
	msgs, err := proposal.GetMsgs()
	if err != nil {
		return RoutePublic
	}
	return ClassifyByMsgType(ctx, h.rs, msgs)
}

func (h VoteHooks) freezeEligibilityOnce(ctx sdk.Context, proposal v1.Proposal) keeper.FrozenEligibility {
	if basis, ok := h.k.GetProposalEligibility(ctx, proposal.Id); ok {
		return basis
	}

	votingStart := proposal.VotingStartTime
	minAge := h.idk.MinIdentityAge(ctx)

	cutoff := int64(0)
	if votingStart != nil {
		cutoff = votingStart.Add(-minAge).Unix()
	}
	basis := keeper.FrozenEligibility{
		Denominator: h.idk.CountEligibleControllersAt(ctx, timeFromUnix(cutoff), 0),
		Cutoff:      cutoff,
		FrozenAt:    ctx.BlockTime().Unix(),
	}
	h.k.SetProposalEligibility(ctx, proposal.Id, basis)
	return basis
}

func (h VoteHooks) AfterProposalSubmission(context.Context, uint64) error       { return nil }
func (h VoteHooks) AfterProposalFailedMinDeposit(context.Context, uint64) error { return nil }

// AfterProposalVotingPeriodEnded queues a FINISHED proposal's vote records for deletion.
func (h VoteHooks) AfterProposalVotingPeriodEnded(ctx context.Context, proposalID uint64) error {
	proposal, err := h.gov.Proposals.Get(ctx, proposalID)
	if err != nil {
		return nil // x/gov surfaces its own errors for a missing proposal
	}
	if !prunableAfterVoting(proposal.Status) {
		return nil
	}
	h.k.EnqueueForPruning(sdk.UnwrapSDKContext(ctx), proposalID)
	return nil
}

func prunableAfterVoting(status v1.ProposalStatus) bool { return status != v1.StatusVotingPeriod }

func timeFromUnix(sec int64) time.Time { return time.Unix(sec, 0) }
