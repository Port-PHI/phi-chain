// SPDX-License-Identifier: Apache-2.0

package governance

import (
	"testing"
	"time"

	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/governance/keeper"
)

type scanTrackingIdentity struct {
	scans  *int
	totals *int
	total  uint64
}

func newScanTracker(total uint64) scanTrackingIdentity {
	s, tot := 0, 0
	return scanTrackingIdentity{scans: &s, totals: &tot, total: total}
}

func (s scanTrackingIdentity) IsEligibleControllerAt(sdk.Context, string, time.Time, time.Duration) bool {
	return true
}

// CountEligibleControllersAt is the tail-walking call.
func (s scanTrackingIdentity) IsEligibleControllerSince(ctx sdk.Context, controller string, t time.Time, minAge time.Duration, _ time.Time) bool {
	return s.IsEligibleControllerAt(ctx, controller, t, minAge)
}

func (s scanTrackingIdentity) CountEligibleControllersAt(sdk.Context, time.Time, time.Duration) uint64 {
	*s.scans++
	return s.total
}

func (s scanTrackingIdentity) EligibleControllerTotal(sdk.Context) uint64 {
	*s.totals++
	return s.total
}

func (s scanTrackingIdentity) MinIdentityAge(sdk.Context) time.Duration { return 0 }

type tallyPath struct {
	name   string
	frozen bool
}

func tallyPaths() []tallyPath {
	return []tallyPath{
		{name: "frozen basis present (the ordinary path)", frozen: true},
		{name: "no frozen basis (the fallback)", frozen: false},
	}
}

// TestTallyFallback_TheEndBlockerNeverWalksTheEligibilityTail drives the tally under an infinite gas meter — the EndBlocker's own meter — on both paths, and asserts the tail scan is never reached.
func TestTallyFallback_TheEndBlockerNeverWalksTheEligibilityTail(t *testing.T) {
	for _, path := range tallyPaths() {
		t.Run(path.name, func(t *testing.T) {
			ctx, k := govSetup(t)
			ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())

			idk := newScanTracker(500)
			proposal := testProposal(1)
			castAll(ctx, k, proposal.Id, []ballot{{1, v1.OptionYes, true}, {2, v1.OptionYes, true}})

			if path.frozen {
				k.SetProposalEligibility(ctx, proposal.Id, keeper.FrozenEligibility{Denominator: 500, Cutoff: 1})
			}

			_, _, err := tallyPublicLive(ctx, k, proposal, idk, testValidators())
			require.NoError(t, err)

			require.Zero(t, *idk.scans,
				"the EndBlocker must never reach the age-ordered tail scan: it has no meter to stop it")
			if path.frozen {
				require.Zero(t, *idk.totals, "with a frozen basis the tally reads neither")
				return
			}
			require.Equal(t, 1, *idk.totals, "the fallback reads the O(1) total exactly once")
		})
	}
}

// The fallback must still produce a usable denominator: treating a missing basis as zero would make scalePublicResults report no power at all, silently rejecting a proposal that may have passed.
func TestTallyFallback_UsesTheTotalAsTheDenominator(t *testing.T) {
	ctx, k := govSetup(t)
	ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())

	idk := newScanTracker(4)
	proposal := testProposal(1)
	castAll(ctx, k, proposal.Id, []ballot{{1, v1.OptionYes, true}, {2, v1.OptionYes, true}})

	power, results, err := tallyPublicLive(ctx, k, proposal, idk, testValidators())
	require.NoError(t, err)

	require.Equal(t, "500.000000000000000000", power.String())
	require.Equal(t, "500.000000000000000000", results[v1.OptionYes].String())
	require.Zero(t, *idk.scans)
}

// The total is an UPPER bound on the age-filtered eligible set — it ignores the minimum-age cutoff — so the fallback denominator is never smaller than the exact one.
func TestTallyFallback_TheTotalIsTheConservativeDenominator(t *testing.T) {
	ctx, k := govSetup(t)
	ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())

	idk := fakeIdentity{denominator: 60, total: 100}

	proposal := testProposal(1)
	castAll(ctx, k, proposal.Id, []ballot{{1, v1.OptionYes, true}})

	power, _, err := tallyPublicLive(ctx, k, proposal, idk, testValidators())
	require.NoError(t, err)

	require.Equal(t, "10.000000000000000000", power.String(),
		"the fallback must divide by the larger total, making quorum harder rather than easier")
}
