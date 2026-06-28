// SPDX-License-Identifier: Apache-2.0

package governance

import (
	"context"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
)

// This file wires the one-human-one-vote tally into x/gov live, without rewriting
// the consensus-critical EndBlocker. The wiring uses the official Cosmos v0.53
// customization hook, govkeeper.WithCustomCalculateVoteResultsAndVotingPowerFn:
// only the "calculate vote results and voting power" function is replaced; the
// entire x/gov proposal/deposit/execution/event lifecycle stays untouched.
//
// The vote route is determined automatically from the proposal message types
// (ClassifyByMsgType):
//   - public    -> one-human-one-vote: each active eligible DID has weight 1; eligibility from the voting-start snapshot.
//   - technical -> validator-weighted: each active validator with its bonded-stake weight (no delegator override).
//
// Quorum trick: x/gov measures quorum as totalVotingPower/totalBonded. For the
// public route we scale each vote by totalBonded/totalEligible so that quorum is
// exactly turnout/totalEligible (per-head counting); the threshold/veto ratios
// are scale-invariant and stay untouched.

// NewPhiTallyFn builds the custom vote-counting function to inject into govkeeper.NewKeeper.
func NewPhiTallyFn(idk IdentitySource) govkeeper.CalculateVoteResultsAndVotingPowerFn {
	return func(ctx context.Context, k govkeeper.Keeper, proposal v1.Proposal, validators map[string]v1.ValidatorGovInfo) (math.LegacyDec, map[v1.VoteOption]math.LegacyDec, error) {
		msgs, err := proposal.GetMsgs()
		if err != nil {
			return math.LegacyZeroDec(), nil, err
		}
		if ClassifyByMsgType(msgs) == RouteTechnical {
			return tallyTechnicalLive(ctx, k, proposal, validators)
		}
		return tallyPublicLive(ctx, k, proposal, idk, validators)
	}
}

// tallyPublicLive is the public route — one-human-one-vote (per-head, scaled for quorum).
func tallyPublicLive(ctx context.Context, k govkeeper.Keeper, proposal v1.Proposal, idk IdentitySource, validators map[string]v1.ValidatorGovInfo) (math.LegacyDec, map[v1.VoteOption]math.LegacyDec, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	minAge := idk.MinIdentityAge(sdkCtx)
	votingStart := time.Time{}
	if proposal.VotingStartTime != nil {
		votingStart = *proposal.VotingStartTime
	}
	totalEligible := idk.CountEligibleControllersAt(sdkCtx, votingStart, minAge)

	counts := map[v1.VoteOption]uint64{}
	var turnout uint64
	counted := map[string]bool{}
	toRemove, err := walkProposalVotes(ctx, k, proposal.Id, func(vote v1.Vote) error {
		if counted[vote.Voter] {
			return nil // one vote per controller (the latest vote is stored)
		}
		if !idk.IsEligibleControllerAt(sdkCtx, vote.Voter, votingStart, minAge) {
			return nil // only active eligible DID (>= min_identity_age in snapshot)
		}
		counted[vote.Voter] = true
		counts[dominantOption(vote.Options)]++
		turnout++
		return nil
	})
	if err != nil {
		return math.LegacyZeroDec(), nil, err
	}
	if err := removeVotes(ctx, k, toRemove); err != nil {
		return math.LegacyZeroDec(), nil, err
	}

	totalPower, results := scalePublicResults(counts, turnout, totalEligible, sumBonded(validators))
	return totalPower, results, nil
}

// tallyTechnicalLive is the technical route — validator-weighted (only validators' own
// votes; no delegator override).
func tallyTechnicalLive(ctx context.Context, k govkeeper.Keeper, proposal v1.Proposal, validators map[string]v1.ValidatorGovInfo) (math.LegacyDec, map[v1.VoteOption]math.LegacyDec, error) {
	byAcc := validatorBondedByAcc(validators)
	var ballots []valBallot
	toRemove, err := walkProposalVotes(ctx, k, proposal.Id, func(vote v1.Vote) error {
		acc, err := sdk.AccAddressFromBech32(vote.Voter)
		if err != nil {
			return err
		}
		bonded, ok := byAcc[string(acc.Bytes())]
		if !ok {
			return nil // voter is not a validator -> ignored on the technical route
		}
		ballots = append(ballots, valBallot{bonded: bonded, options: vote.Options})
		return nil
	})
	if err != nil {
		return math.LegacyZeroDec(), nil, err
	}
	if err := removeVotes(ctx, k, toRemove); err != nil {
		return math.LegacyZeroDec(), nil, err
	}
	return tallyValidatorBallots(ballots)
}

// --- pure helpers (testable without the gov keeper) ---

// emptyResults returns a zeroed four-option results map.
func emptyResults() map[v1.VoteOption]math.LegacyDec {
	return map[v1.VoteOption]math.LegacyDec{
		v1.OptionYes:        math.LegacyZeroDec(),
		v1.OptionAbstain:    math.LegacyZeroDec(),
		v1.OptionNo:         math.LegacyZeroDec(),
		v1.OptionNoWithVeto: math.LegacyZeroDec(),
	}
}

// dominantOption returns the highest-weight option; an empty or tied vote -> Abstain (anti-ambiguity).
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

// scalePublicResults converts per-head counts into scaled voting power so that the
// totalPower/totalBonded quorum equals exactly turnout/totalEligible.
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

// valBallot is a validator vote with its bonded-stake weight.
type valBallot struct {
	bonded  math.Int
	options []*v1.WeightedVoteOption
}

// tallyValidatorBallots distributes each validator's power across its option weights (stake-weighted).
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

// sumBonded is the total bonded stake of all active validators (= staking totalBonded per the invariant).
func sumBonded(validators map[string]v1.ValidatorGovInfo) math.Int {
	t := math.ZeroInt()
	for _, v := range validators {
		t = t.Add(v.BondedTokens)
	}
	return t
}

// validatorBondedByAcc maps validator account address bytes -> bonded stake (for voter matching).
func validatorBondedByAcc(validators map[string]v1.ValidatorGovInfo) map[string]math.Int {
	m := make(map[string]math.Int, len(validators))
	for _, v := range validators {
		m[string(v.Address.Bytes())] = v.BondedTokens
	}
	return m
}

// walkProposalVotes iterates over a proposal's votes and collects their keys for post-tally removal.
func walkProposalVotes(ctx context.Context, k govkeeper.Keeper, proposalID uint64, fn func(v1.Vote) error) ([]collections.Pair[uint64, sdk.AccAddress], error) {
	rng := collections.NewPrefixedPairRange[uint64, sdk.AccAddress](proposalID)
	var toRemove []collections.Pair[uint64, sdk.AccAddress]
	err := k.Votes.Walk(ctx, rng, func(key collections.Pair[uint64, sdk.AccAddress], vote v1.Vote) (bool, error) {
		toRemove = append(toRemove, key)
		if err := fn(vote); err != nil {
			return true, err
		}
		return false, nil
	})
	return toRemove, err
}

// removeVotes deletes the counted votes from the store (like the default tally).
func removeVotes(ctx context.Context, k govkeeper.Keeper, keys []collections.Pair[uint64, sdk.AccAddress]) error {
	for _, key := range keys {
		if err := k.Votes.Remove(ctx, key); err != nil {
			return err
		}
	}
	return nil
}
