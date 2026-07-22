// SPDX-License-Identifier: Apache-2.0

package governance

import (
	"fmt"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"
)

type countingIdentity struct {
	calls       *int
	denominator *uint64
	eligible    map[string]bool
	minAge      time.Duration
}

func newCountingIdentity(start uint64) countingIdentity {
	calls, den := 0, start
	return countingIdentity{calls: &calls, denominator: &den, eligible: map[string]bool{}}
}

func (c countingIdentity) IsEligibleControllerAt(_ sdk.Context, controller string, _ time.Time, _ time.Duration) bool {
	return c.eligible[controller]
}

// CountEligibleControllersAt models a registry that keeps growing while voting is open: every call answers with one more eligible controller than the last.
func (c countingIdentity) IsEligibleControllerSince(ctx sdk.Context, controller string, t time.Time, minAge time.Duration, _ time.Time) bool {
	return c.IsEligibleControllerAt(ctx, controller, t, minAge)
}

func (c countingIdentity) CountEligibleControllersAt(_ sdk.Context, _ time.Time, _ time.Duration) uint64 {
	*c.calls++
	answer := *c.denominator
	*c.denominator = answer + 1
	return answer
}

// EligibleControllerTotal is never consulted by the freeze path; it is present only to satisfy the interface, and answers loudly enough that a test would notice if the freeze ever used it.
func (c countingIdentity) EligibleControllerTotal(_ sdk.Context) uint64 { return 1 << 40 }

func (c countingIdentity) MinIdentityAge(_ sdk.Context) time.Duration { return c.minAge }

type depositSequence struct {
	name          string
	laterDeposits int
	advanceBlocks bool
}

func depositSequences() []depositSequence {
	return []depositSequence{
		{name: "no further deposits", laterDeposits: 0},
		{name: "one further deposit, same block", laterDeposits: 1},
		{name: "one further deposit, later block", laterDeposits: 1, advanceBlocks: true},
		{name: "several further deposits, same block", laterDeposits: 5},
		{name: "several further deposits, later blocks", laterDeposits: 5, advanceBlocks: true},
		{name: "many further deposits across many blocks", laterDeposits: 50, advanceBlocks: true},
	}
}

// TestFreezeOnce_LaterDepositsNeverResnapshotTheDenominator drives every shape of post-start deposit and asserts the denominator is captured exactly once.
func TestFreezeOnce_LaterDepositsNeverResnapshotTheDenominator(t *testing.T) {
	for _, seq := range depositSequences() {
		t.Run(seq.name, func(t *testing.T) {
			ctx, k := govSetup(t)
			ctx = ctx.WithBlockTime(time.Unix(1_000_000, 0)).WithBlockHeight(10)

			idk := newCountingIdentity(100)
			h := NewVoteHooks(k, nil, idk, publicRoutes{}, noValidators{})
			proposal := testProposal(7)

			first := h.freezeEligibilityOnce(ctx, proposal)
			require.Equal(t, uint64(100), first.Denominator, "the first snapshot is the registry as it stood")
			require.Equal(t, 1, *idk.calls)

			for i := 0; i < seq.laterDeposits; i++ {
				if seq.advanceBlocks {
					ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1).
						WithBlockTime(ctx.BlockTime().Add(6 * time.Second))
				}
				got := h.freezeEligibilityOnce(ctx, proposal)
				require.Equal(t, first, got,
					"deposit %d must return the ORIGINAL basis, not a fresh one", i+1)
			}

			require.Equal(t, 1, *idk.calls,
				"the denominator must be computed exactly once, however many deposits arrive")
			stored, ok := k.GetProposalEligibility(ctx, proposal.Id)
			require.True(t, ok)
			require.Equal(t, first, stored, "the stored basis must still be the first one taken")
			require.Greater(t, *idk.denominator, uint64(100),
				"the registry must genuinely have moved, or this proves nothing")
		})
	}
}

// The vote hook shares the same guard.
func TestFreezeOnce_TheVoteHookSharesTheGuard(t *testing.T) {
	t.Run("basis already frozen by a deposit", func(t *testing.T) {
		ctx, k := govSetup(t)
		idk := newCountingIdentity(42)
		h := NewVoteHooks(k, nil, idk, publicRoutes{}, noValidators{})
		proposal := testProposal(1)

		first := h.freezeEligibilityOnce(ctx, proposal)
		for i := 0; i < 10; i++ {
			require.Equal(t, first, h.freezeEligibilityOnce(ctx, proposal))
		}
		require.Equal(t, 1, *idk.calls)
	})

	t.Run("proposal that never passed through the deposit hook", func(t *testing.T) {
		ctx, k := govSetup(t)
		idk := newCountingIdentity(77)
		h := NewVoteHooks(k, nil, idk, publicRoutes{}, noValidators{})
		proposal := testProposal(2)

		_, ok := k.GetProposalEligibility(ctx, proposal.Id)
		require.False(t, ok, "no basis exists yet")

		basis := h.freezeEligibilityOnce(ctx, proposal)
		require.Equal(t, uint64(77), basis.Denominator, "the first ballot must still freeze a basis")
		stored, ok := k.GetProposalEligibility(ctx, proposal.Id)
		require.True(t, ok)
		require.Equal(t, basis, stored)
		require.Equal(t, 1, *idk.calls)
	})
}

// Proposals do not share a basis: freezing one must not suppress freezing another.
func TestFreezeOnce_EachProposalFreezesIndependently(t *testing.T) {
	ctx, k := govSetup(t)
	idk := newCountingIdentity(1_000)
	h := NewVoteHooks(k, nil, idk, publicRoutes{}, noValidators{})

	seen := map[uint64]uint64{}
	for id := uint64(1); id <= 5; id++ {
		basis := h.freezeEligibilityOnce(ctx, testProposal(id))
		seen[id] = basis.Denominator
	}
	require.Equal(t, 5, *idk.calls, "each proposal takes its own single snapshot")

	for id := uint64(1); id <= 5; id++ {
		require.Equal(t, seen[id], h.freezeEligibilityOnce(ctx, testProposal(id)).Denominator,
			fmt.Sprintf("proposal %d must keep its own frozen denominator", id))
	}
	require.Equal(t, 5, *idk.calls)
}

// The cutoff is frozen alongside the denominator, so a mid-vote change to min_identity_age cannot move it either — two voters casting identical ballots at different moments are judged by one rule.
func TestFreezeOnce_TheCutoffIsFrozenToo(t *testing.T) {
	ctx, k := govSetup(t)
	ctx = ctx.WithBlockTime(time.Unix(1_000_000, 0))

	idk := newCountingIdentity(10)
	idk.minAge = 24 * time.Hour
	h := NewVoteHooks(k, nil, idk, publicRoutes{}, noValidators{})

	start := time.Unix(2_000_000, 0)
	proposal := v1.Proposal{Id: 9, VotingStartTime: &start}

	basis := h.freezeEligibilityOnce(ctx, proposal)
	require.Equal(t, start.Add(-24*time.Hour).Unix(), basis.Cutoff)

	idk.minAge = 30 * 24 * time.Hour
	require.Equal(t, basis, h.freezeEligibilityOnce(ctx, proposal),
		"a mid-vote parameter change must not move an in-flight proposal's cutoff")
}
