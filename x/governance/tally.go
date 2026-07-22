// SPDX-License-Identifier: Apache-2.0

// Package governance holds Phi's one-human-one-vote tally logic.
package governance

import (
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"github.com/Port-PHI/phi-chain/x/governance/types"
)

// VoteRoute is the vote route of a proposal.
type VoteRoute = types.VoteRoute

const (
	// RoutePublic is the public route — one-human-one-vote.
	RoutePublic = types.VOTE_ROUTE_PUBLIC
	// RouteTechnical is the technical route — validator stake-weighted voting.
	RouteTechnical = types.VOTE_ROUTE_TECHNICAL
)

// IdentitySource abstracts the identity keeper for tally.
type IdentitySource interface {
	IsEligibleControllerAt(ctx sdk.Context, controller string, t time.Time, minAge time.Duration) bool
	// IsEligibleControllerSince is the membership test of a FROZEN basis: eligible at cutoff t, and continuously so since the basis was taken.
	IsEligibleControllerSince(ctx sdk.Context, controller string, t time.Time, minAge time.Duration, since time.Time) bool
	CountEligibleControllersAt(ctx sdk.Context, t time.Time, minAge time.Duration) uint64
	// EligibleControllerTotal is the O(1) count of controllers holding at least one ACTIVE DID, read straight from a stored counter with no scan of any kind.
	EligibleControllerTotal(ctx sdk.Context) uint64
	MinIdentityAge(ctx sdk.Context) time.Duration
}

// RouteSource abstracts the governance keeper: it supplies the GOVERNED message-type → path table.
type RouteSource interface {
	VoteRouteTable(ctx sdk.Context) map[string]types.VoteRoute
}

// ClassifyByMsgType determines the vote route automatically from the proposal's message types (no human discretion), against the governed table.
func ClassifyByMsgType(ctx sdk.Context, rs RouteSource, msgs []sdk.Msg) VoteRoute {
	return types.Classify(msgs, rs.VoteRouteTable(ctx))
}

// VoteRecord is a recorded vote (the latest vote of each controller).
type VoteRecord struct {
	Voter  string // DID controller address
	Option govv1.VoteOption
}

// OneHumanTally is the result of a one-human-one-vote count.
type OneHumanTally struct {
	Yes           uint64
	No            uint64
	NoWithVeto    uint64
	Abstain       uint64
	TotalEligible uint64 // total eligible DIDs in the voting-start snapshot
}

// TallyOneHumanOneVote weights each vote as 1 per active eligible DID.
func TallyOneHumanOneVote(ctx sdk.Context, idk IdentitySource, votingStart time.Time, votes []VoteRecord) OneHumanTally {
	minAge := idk.MinIdentityAge(ctx)
	res := OneHumanTally{TotalEligible: idk.CountEligibleControllersAt(ctx, votingStart, minAge)}

	counted := make(map[string]bool)
	for _, v := range votes {
		if counted[v.Voter] {
			continue
		}
		if !idk.IsEligibleControllerAt(ctx, v.Voter, votingStart, minAge) {
			continue
		}
		counted[v.Voter] = true
		switch v.Option {
		case govv1.OptionYes:
			res.Yes++
		case govv1.OptionNo:
			res.No++
		case govv1.OptionNoWithVeto:
			res.NoWithVeto++
		case govv1.OptionAbstain:
			res.Abstain++
		}
	}
	return res
}

// Passes checks proposal approval against quorum/threshold/veto threshold (x/gov model, but based on per-head counting).
func (t OneHumanTally) Passes(quorum, threshold, vetoThreshold math.LegacyDec) bool {
	if t.TotalEligible == 0 {
		return false
	}
	totalVotes := t.Yes + t.No + t.NoWithVeto + t.Abstain
	if totalVotes == 0 {
		return false
	}
	// Quorum: ratio of turnout to total eligible.
	turnout := decFromU64(totalVotes).Quo(decFromU64(t.TotalEligible))
	if turnout.LT(quorum) {
		return false
	}
	// Veto: ratio of NoWithVeto to total votes.
	veto := decFromU64(t.NoWithVeto).Quo(decFromU64(totalVotes))
	if veto.GT(vetoThreshold) {
		return false
	}
	// Threshold: ratio of Yes to non-abstain votes.
	nonAbstain := t.Yes + t.No + t.NoWithVeto
	if nonAbstain == 0 {
		return false
	}
	yesRatio := decFromU64(t.Yes).Quo(decFromU64(nonAbstain))
	return yesRatio.GT(threshold)
}

func decFromU64(x uint64) math.LegacyDec {
	return math.LegacyNewDecFromInt(math.NewIntFromUint64(x))
}
