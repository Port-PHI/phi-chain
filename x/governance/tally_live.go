// SPDX-License-Identifier: Apache-2.0

package governance

import (
	"context"
	"sort"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"github.com/Port-PHI/phi-chain/x/governance/keeper"
)

// Wires the one-human-one-vote tally into x/gov via the v0.53 custom CalculateVoteResultsAndVotingPowerFn hook; the rest of the x/gov lifecycle is untouched.

// NewPhiTallyFn builds the custom vote-counting function injected into govkeeper.NewKeeper.
func NewPhiTallyFn(gk keeper.Keeper, idk IdentitySource, rs RouteSource) govkeeper.CalculateVoteResultsAndVotingPowerFn {
	return func(ctx context.Context, k govkeeper.Keeper, proposal v1.Proposal, validators map[string]v1.ValidatorGovInfo) (math.LegacyDec, map[v1.VoteOption]math.LegacyDec, error) {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		msgs, err := proposal.GetMsgs()
		if err != nil {
			return math.LegacyZeroDec(), nil, err
		}

		// Queue vote records for deletion only if this tally ends the proposal.
		if prunableAtTally(proposal) {
			gk.EnqueueForPruning(sdkCtx, proposal.Id)
		}

		if ClassifyByMsgType(sdkCtx, rs, msgs) == RouteTechnical {
			return tallyTechnicalLive(ctx, k, proposal, validators)
		}
		return tallyPublicLive(sdkCtx, gk, proposal, idk, validators)
	}
}

func prunableAtTally(proposal v1.Proposal) bool { return !proposal.Expedited }

func tallyPublicLive(ctx sdk.Context, gk keeper.Keeper, proposal v1.Proposal, idk IdentitySource, validators map[string]v1.ValidatorGovInfo) (math.LegacyDec, map[v1.VoteOption]math.LegacyDec, error) {
	running := gk.GetRunningTally(ctx, proposal.Id)

	counts := map[v1.VoteOption]uint64{}
	for option, n := range running.Counts {
		counts[v1.VoteOption(option)] = n
	}

	// Frozen denominator; if missing, fall back to the O(1) eligible-controller total (never zero, which would report full quorum) — an upper bound, so quorum is harder, and read not recomputed (infinite gas meter).
	totalEligible := uint64(0)
	if basis, ok := gk.GetProposalEligibility(ctx, proposal.Id); ok {
		totalEligible = basis.Denominator
	} else {
		totalEligible = idk.EligibleControllerTotal(ctx)
	}

	totalPower, results := scalePublicResults(counts, running.Turnout, totalEligible, sumBonded(validators))
	return totalPower, results, nil
}

func tallyTechnicalLive(ctx context.Context, k govkeeper.Keeper, proposal v1.Proposal, validators map[string]v1.ValidatorGovInfo) (math.LegacyDec, map[v1.VoteOption]math.LegacyDec, error) {
	operators := make([]string, 0, len(validators))
	for operator := range validators {
		operators = append(operators, operator)
	}
	sort.Strings(operators)

	var ballots []valBallot
	for _, operator := range operators {
		v := validators[operator]
		vote, err := k.Votes.Get(ctx, collections.Join(proposal.Id, sdk.AccAddress(v.Address)))
		if err != nil {
			continue // this validator did not vote
		}
		ballots = append(ballots, valBallot{bonded: v.BondedTokens, options: vote.Options})
	}
	return tallyValidatorBallots(ballots)
}

func emptyResults() map[v1.VoteOption]math.LegacyDec {
	return map[v1.VoteOption]math.LegacyDec{
		v1.OptionYes:        math.LegacyZeroDec(),
		v1.OptionAbstain:    math.LegacyZeroDec(),
		v1.OptionNo:         math.LegacyZeroDec(),
		v1.OptionNoWithVeto: math.LegacyZeroDec(),
	}
}

func dominantOption(opts []*v1.WeightedVoteOption) v1.VoteOption {
	best := v1.OptionAbstain
	bestW := math.LegacyZeroDec()
	tie := false
	for _, o := range opts {
		w, err := math.LegacyNewDecFromStr(o.Weight)
		if err != nil {
			continue
		}
		switch {
		case w.GT(bestW):
			bestW, best, tie = w, o.Option, false
		case w.Equal(bestW) && !bestW.IsZero():
			tie = true
		}
	}
	if tie {
		return v1.OptionAbstain
	}
	return best
}

func scalePublicResults(counts map[v1.VoteOption]uint64, turnout, totalEligible uint64, totalBonded math.Int) (math.LegacyDec, map[v1.VoteOption]math.LegacyDec) {
	results := emptyResults()
	if turnout == 0 || totalEligible == 0 || totalBonded.IsZero() {
		return math.LegacyZeroDec(), results
	}
	scale := math.LegacyNewDecFromInt(totalBonded).Quo(math.LegacyNewDecFromInt(math.NewIntFromUint64(totalEligible)))
	for opt, c := range counts {
		results[opt] = math.LegacyNewDecFromInt(math.NewIntFromUint64(c)).Mul(scale)
	}
	totalPower := math.LegacyNewDecFromInt(math.NewIntFromUint64(turnout)).Mul(scale)
	return totalPower, results
}

type valBallot struct {
	bonded  math.Int
	options []*v1.WeightedVoteOption
}

func tallyValidatorBallots(ballots []valBallot) (math.LegacyDec, map[v1.VoteOption]math.LegacyDec, error) {
	results := emptyResults()
	totalPower := math.LegacyZeroDec()
	for _, b := range ballots {
		power := math.LegacyNewDecFromInt(b.bonded)
		for _, o := range b.options {
			w, err := math.LegacyNewDecFromStr(o.Weight)
			if err != nil {
				return math.LegacyZeroDec(), nil, err
			}
			results[o.Option] = results[o.Option].Add(power.Mul(w))
		}
		totalPower = totalPower.Add(power)
	}
	return totalPower, results, nil
}

func sumBonded(validators map[string]v1.ValidatorGovInfo) math.Int {
	t := math.ZeroInt()
	for _, v := range validators {
		t = t.Add(v.BondedTokens)
	}
	return t
}
