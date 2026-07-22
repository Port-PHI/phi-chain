// SPDX-License-Identifier: Apache-2.0

package governance

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/governance/types"
)

type mutableIdentity struct {
	eligible map[string]bool
	den      uint64
}

func newMutableIdentity(den uint64) *mutableIdentity {
	return &mutableIdentity{eligible: map[string]bool{}, den: den}
}

func (m *mutableIdentity) IsEligibleControllerAt(_ sdk.Context, controller string, _ time.Time, _ time.Duration) bool {
	return m.eligible[controller]
}

// IsEligibleControllerSince adds the frozen-basis continuity test.
func (m *mutableIdentity) IsEligibleControllerSince(ctx sdk.Context, controller string, t time.Time, minAge time.Duration, _ time.Time) bool {
	return m.IsEligibleControllerAt(ctx, controller, t, minAge)
}

func (m *mutableIdentity) CountEligibleControllersAt(_ sdk.Context, _ time.Time, _ time.Duration) uint64 {
	return m.den
}
func (m *mutableIdentity) EligibleControllerTotal(_ sdk.Context) uint64 { return m.den }

func (m *mutableIdentity) MinIdentityAge(_ sdk.Context) time.Duration { return 0 }

func weighted(opt v1.VoteOption) []*v1.WeightedVoteOption {
	return []*v1.WeightedVoteOption{{Option: opt, Weight: "1.0"}}
}

type castOutcome struct {
	name     string
	eligible bool
	wantErr  bool
}

func castOutcomes() []castOutcome {
	return []castOutcome{
		{name: "eligible at cast time", eligible: true, wantErr: false},
		{name: "ineligible at cast time", eligible: false, wantErr: true},
	}
}

// TestVoteEligibility_IneligibleBallotIsRefusedByName walks both verdicts and asserts the refusal is an error the voter can see — and that a refused ballot leaves the tally completely untouched.
func TestVoteEligibility_IneligibleBallotIsRefusedByName(t *testing.T) {
	for _, tc := range castOutcomes() {
		t.Run(tc.name, func(t *testing.T) {
			ctx, k := govSetup(t)
			idk := newMutableIdentity(10)
			h := NewVoteHooks(k, nil, idk, publicRoutes{}, noValidators{})
			proposal := testProposal(1)

			voter := "phi1voter"
			addr := []byte("voter-00000000000001")
			idk.eligible[voter] = tc.eligible

			err := h.recordBallot(ctx, proposal, voter, addr, weighted(v1.OptionYes))

			tally := k.GetRunningTally(ctx, proposal.Id)
			if tc.wantErr {
				require.ErrorIs(t, err, types.ErrNotEligibleToVote,
					"an ineligible ballot must be refused by name, not silently accepted")
				require.Zero(t, tally.Turnout, "a refused ballot must not move the tally")
				require.Zero(t, tally.Counts[int32(v1.OptionYes)])
				_, marked := k.GetCountedVote(ctx, proposal.Id, addr)
				require.False(t, marked,
					"a refused ballot must leave no record, or the refusal would be sticky")
				return
			}
			require.NoError(t, err)
			require.Equal(t, uint64(1), tally.Turnout)
			require.Equal(t, uint64(1), tally.Counts[int32(v1.OptionYes)])
		})
	}
}

// The refusal must not lock the voter out.
func TestVoteEligibility_ALaterEligibleVoterCanStillCast(t *testing.T) {
	ctx, k := govSetup(t)
	idk := newMutableIdentity(10)
	h := NewVoteHooks(k, nil, idk, publicRoutes{}, noValidators{})
	proposal := testProposal(1)

	voter := "phi1latecomer"
	addr := []byte("voter-00000000000002")

	for i := 0; i < 3; i++ {
		require.ErrorIs(t, h.recordBallot(ctx, proposal, voter, addr, weighted(v1.OptionYes)),
			types.ErrNotEligibleToVote)
	}
	require.Zero(t, k.GetRunningTally(ctx, proposal.Id).Turnout)

	idk.eligible[voter] = true
	require.NoError(t, h.recordBallot(ctx, proposal, voter, addr, weighted(v1.OptionNo)),
		"a voter who becomes eligible within the window must still be able to cast")

	tally := k.GetRunningTally(ctx, proposal.Id)
	require.Equal(t, uint64(1), tally.Turnout)
	require.Equal(t, uint64(1), tally.Counts[int32(v1.OptionNo)])
}

type castEvent struct {
	voter    int
	option   v1.VoteOption
	eligible bool
}

func frozenRecount(events []castEvent) (counts map[v1.VoteOption]uint64, turnout uint64) {
	latest := map[int]v1.VoteOption{}
	for _, e := range events {
		if !e.eligible {
			continue // refused at the hook: no state is written, so nothing moves
		}
		latest[e.voter] = e.option
	}
	counts = map[v1.VoteOption]uint64{}
	for _, opt := range latest {
		counts[opt]++
	}
	return counts, uint64(len(latest))
}

// TestVoteEligibility_IncrementalEqualsFrozenRecount is the proof.
func TestVoteEligibility_IncrementalEqualsFrozenRecount(t *testing.T) {
	options := []v1.VoteOption{v1.OptionYes, v1.OptionAbstain, v1.OptionNo, v1.OptionNoWithVeto}

	for _, seed := range []int64{1, 2, 3, 7, 11, 42, 1337, 90210} {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			ctx, k := govSetup(t)
			idk := newMutableIdentity(1_000)
			h := NewVoteHooks(k, nil, idk, publicRoutes{}, noValidators{})
			proposal := testProposal(1)

			const voters = 40
			addrOf := func(i int) []byte { return []byte(fmt.Sprintf("voter-%014d", i)) }
			nameOf := func(i int) string { return fmt.Sprintf("phi1voter%d", i) }

			for i := 0; i < voters; i++ {
				idk.eligible[nameOf(i)] = rng.Intn(2) == 0
			}

			var events []castEvent
			for step := 0; step < 400; step++ {
				if rng.Intn(4) == 0 {
					v := rng.Intn(voters)
					idk.eligible[nameOf(v)] = !idk.eligible[nameOf(v)]
					continue
				}
				v := rng.Intn(voters)
				opt := options[rng.Intn(len(options))]
				eligibleNow := idk.eligible[nameOf(v)]

				err := h.recordBallot(ctx, proposal, nameOf(v), addrOf(v), weighted(opt))
				if eligibleNow {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, types.ErrNotEligibleToVote)
				}
				events = append(events, castEvent{voter: v, option: opt, eligible: eligibleNow})
			}

			wantCounts, wantTurnout := frozenRecount(events)
			got := k.GetRunningTally(ctx, proposal.Id)

			require.Equal(t, wantTurnout, got.Turnout,
				"the accumulated turnout must equal a frozen from-scratch recount")
			for _, opt := range options {
				require.Equal(t, wantCounts[opt], got.Counts[int32(opt)],
					"option %s: accumulator must equal the frozen recount exactly", opt)
			}
			for opt, n := range got.Counts {
				require.Contains(t, []int32{1, 2, 3, 4}, opt, "unexpected option %d with count %d", opt, n)
			}
		})
	}
}

// The frozen-versus-current gap, asserted as INTENDED rather than discovered.
func TestVoteEligibility_FrozenVersusCurrentDifferenceIsIntended(t *testing.T) {
	ctx, k := govSetup(t)
	idk := newMutableIdentity(100)
	h := NewVoteHooks(k, nil, idk, publicRoutes{}, noValidators{})
	proposal := testProposal(1)

	const total, revokedAfterVoting = 40, 9
	addrOf := func(i int) []byte { return []byte(fmt.Sprintf("voter-%014d", i)) }
	nameOf := func(i int) string { return fmt.Sprintf("phi1voter%d", i) }

	var events []castEvent
	for i := 0; i < total; i++ {
		idk.eligible[nameOf(i)] = true
		require.NoError(t, h.recordBallot(ctx, proposal, nameOf(i), addrOf(i), weighted(v1.OptionYes)))
		events = append(events, castEvent{voter: i, option: v1.OptionYes, eligible: true})
	}

	for i := 0; i < revokedAfterVoting; i++ {
		idk.eligible[nameOf(i)] = false
	}

	got := k.GetRunningTally(ctx, proposal.Id)

	wantCounts, wantTurnout := frozenRecount(events)
	require.Equal(t, wantTurnout, got.Turnout)
	require.Equal(t, wantCounts[v1.OptionYes], got.Counts[int32(v1.OptionYes)])
	require.Equal(t, uint64(total), got.Turnout, "every ballot was valid when it was cast")

	currentRecount := uint64(0)
	for i := 0; i < total; i++ {
		if idk.eligible[nameOf(i)] {
			currentRecount++
		}
	}
	require.Equal(t, uint64(total-revokedAfterVoting), currentRecount)
	require.Equal(t, got.Turnout-currentRecount, uint64(revokedAfterVoting),
		"the frozen/current gap is exactly the voters revoked after casting — the frozen rule, not a bug")
	require.Greater(t, got.Turnout, currentRecount,
		"frozen counting keeps ballots that were legitimate when cast")
}
