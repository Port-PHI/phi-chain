// SPDX-License-Identifier: Apache-2.0

// Package governance holds Phi's one-human-one-vote tally logic.
//
// Design: automatic vote-route classification based on
// the proposal message type (no human discretion). The "public" route weights
// each vote as 1 per active DID (not balance); eligibility is taken from the
// snapshot at voting start (DID with age >= min_identity_age). The "technical"
// route uses validator stake-weighted voting.
//
// Wiring status: stock Cosmos x/gov has no override point for tally (EndBlocker calls
// keeper.Tally directly), so this logic lives in a standalone, tested package and is wired
// as the live tally via the custom tally function injected in app.go
// (governance.NewPhiTallyFn). The standalone TallyOneHumanOneVote remains for unit testing.
package governance

import (
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
)

// VoteRoute is the vote route of a proposal.
type VoteRoute int

const (
	// RoutePublic is the public route — one-human-one-vote.
	RoutePublic VoteRoute = iota
	// RouteTechnical is the technical route — validator stake-weighted voting.
	RouteTechnical
)

// IdentitySource abstracts the identity keeper for tally.
type IdentitySource interface {
	IsEligibleControllerAt(ctx sdk.Context, controller string, t time.Time, minAge time.Duration) bool
	CountEligibleControllersAt(ctx sdk.Context, t time.Time, minAge time.Duration) uint64
	MinIdentityAge(ctx sdk.Context) time.Duration
}

// TechnicalMsgTypeURLs is the default set of "technical" types; everything else is public.
// This map changes only by public vote (anti governance-capture).
//
// Consensus-critical operations (chain upgrades, consensus params) are routed to the
// validator stake-weighted path rather than the per-DID public path: a chain halt/upgrade is
// an operational decision for the node operators who must run the new binary, and routing it
// through the per-head vote (over a DID set that proposers may influence) is a governance-capture
// and liveness risk. Policy/economic changes remain on the public one-human-one-vote route.
var TechnicalMsgTypeURLs = map[string]bool{
	"/cosmos.staking.v1beta1.MsgUpdateParams":      true,
	"/cosmos.slashing.v1beta1.MsgUpdateParams":     true,
	"/cosmos.distribution.v1beta1.MsgUpdateParams": true,
	"/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade":   true,
	"/cosmos.upgrade.v1beta1.MsgCancelUpgrade":     true,
	"/cosmos.consensus.v1.MsgUpdateParams":         true,
}

// ClassifyByMsgType determines the vote route automatically from the proposal message
// types (no human discretion): if any message is technical the route is technical,
// otherwise public.
func ClassifyByMsgType(msgs []sdk.Msg) VoteRoute {
	for _, m := range msgs {
		if TechnicalMsgTypeURLs[sdk.MsgTypeURL(m)] {
			return RouteTechnical
		}
	}
	return RoutePublic
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
// Eligibility is measured from the voting-start snapshot (active DID with age >= min_identity_age).
// Duplicate votes from one controller are counted only once.
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

// Passes checks proposal approval against quorum/threshold/veto threshold (x/gov model,
// but based on per-head counting).
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

// decFromU64 converts a uint64 count to a LegacyDec without the int64 overflow of a direct cast
// Matching the live tally path (tally_live.go uses math.NewIntFromUint64).
func decFromU64(x uint64) math.LegacyDec {
	return math.LegacyNewDecFromInt(math.NewIntFromUint64(x))
}
