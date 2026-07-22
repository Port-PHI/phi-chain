// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

func setStandingDoc(k Keeper, ctx sdk.Context, did, controller string, createdAt int64, s types.DIDStatus) {
	k.SetIdentity(ctx, types.DIDDocument{Did: did, Controller: controller, Status: s, CreatedAt: createdAt, PubKey: []byte("pk")})
}

// THE OLD UNSOUND CORNER, PROVED CLOSED.
func TestReinstateAdversarial_SuspendingTheOlderOfTwoDIDsNeverDipsTheStanding(t *testing.T) {
	ctx, k := setupInternal(t)
	const ctrl = "phi1twodids"
	const older, younger = int64(100), int64(500)

	ctx = ctx.WithBlockTime(time.Unix(1_000, 0))
	setStandingDoc(k, ctx, "did:phi:old", ctrl, older, types.DID_STATUS_ACTIVE)
	setStandingDoc(k, ctx, "did:phi:young", ctrl, younger, types.DID_STATUS_ACTIVE)

	require.True(t, k.IsEligibleControllerAt(ctx, ctrl, time.Unix(older, 0), 0))

	frozenActive := time.Unix(1_000, 0)
	require.True(t, k.IsEligibleControllerSince(ctx, ctrl, time.Unix(older, 0), 0, frozenActive))

	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	setStandingDoc(k, ctx, "did:phi:old", ctrl, older, types.DID_STATUS_SUSPENDED)
	require.Equal(t, uint64(1), k.CountEligibleControllersAt(ctx, time.Unix(older, 0), 0),
		"the controller is still counted at its earliest DID while the older DID is suspended")

	frozenDuring := time.Unix(2_000, 0)
	require.True(t, k.IsEligibleControllerSince(ctx, ctrl, time.Unix(older, 0), 0, frozenDuring))

	ctx = ctx.WithBlockTime(time.Unix(3_000, 0))
	setStandingDoc(k, ctx, "did:phi:old", ctrl, older, types.DID_STATUS_ACTIVE)

	require.True(t, k.IsEligibleControllerSince(ctx, ctrl, time.Unix(older, 0), 0, frozenActive))
	require.True(t, k.IsEligibleControllerSince(ctx, ctrl, time.Unix(older, 0), 0, frozenDuring))
}

// A GENUINE GAP still restarts.
func TestReinstateAdversarial_RevokeThenReRegisterRestartsStanding(t *testing.T) {
	ctx, k := setupInternal(t)
	const ctrl = "phi1gap"

	ctx = ctx.WithBlockTime(time.Unix(1_000, 0))
	setStandingDoc(k, ctx, "did:phi:g1", ctrl, 100, types.DID_STATUS_ACTIVE)

	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	setStandingDoc(k, ctx, "did:phi:g1", ctrl, 100, types.DID_STATUS_REVOKED)
	require.Zero(t, k.EligibleControllerTotal(ctx), "revocation empties the basis")

	frozenDuringGap := time.Unix(3_000, 0)
	require.Zero(t, k.CountEligibleControllersAt(ctx, time.Unix(100, 0), 0))

	ctx = ctx.WithBlockTime(time.Unix(4_000, 0))
	setStandingDoc(k, ctx, "did:phi:g2", ctrl, 100, types.DID_STATUS_ACTIVE)

	require.True(t, k.IsEligibleControllerAt(ctx, ctrl, time.Unix(100, 0), 0))
	require.False(t, k.IsEligibleControllerSince(ctx, ctrl, time.Unix(100, 0), 0, frozenDuringGap),
		"a controller that regained standing after a gap was not in a basis frozen during the gap")
}

// B-1 STAYS FIXED at the keeper level: a genuine retroactive improvement — an OLDER DID arriving on a controller that was NOT in a frozen denominator — still restarts eligible_since, so it cannot enter that basis.
func TestReinstateAdversarial_OlderDIDArrivalStillRestarts(t *testing.T) {
	ctx, k := setupInternal(t)
	const ctrl = "phi1improve"

	young := int64(5_000_000)
	ctx = ctx.WithBlockTime(time.Unix(young, 0))
	setStandingDoc(k, ctx, "did:phi:young", ctrl, young, types.DID_STATUS_ACTIVE)

	frozenAt := young + 1_000
	cutoff := time.Unix(young-1, 0)
	require.False(t, k.IsEligibleControllerSince(ctx, ctrl, cutoff, 0, time.Unix(frozenAt, 0)),
		"the young controller is outside the frozen denominator")

	ctx = ctx.WithBlockTime(time.Unix(frozenAt+1_000, 0))
	setStandingDoc(k, ctx, "did:phi:old", ctrl, young-100_000, types.DID_STATUS_ACTIVE)

	require.True(t, k.IsEligibleControllerAt(ctx, ctrl, cutoff, 0), "the recovered DID genuinely predates the cutoff")
	require.False(t, k.IsEligibleControllerSince(ctx, ctrl, cutoff, 0, time.Unix(frozenAt, 0)),
		"a retroactive improvement must not be claimable against a basis frozen before it — B-1 stays fixed")
}

// A suspend→reinstate of a controller that was NOT in the denominator (young) must not sneak it in: the standing is unchanged (still young), so a basis whose cutoff predates the young DID still excludes it.
func TestReinstateAdversarial_SuspendReinstateCannotSmuggleAYoungControllerIn(t *testing.T) {
	ctx, k := setupInternal(t)
	const ctrl = "phi1young"

	young := int64(5_000_000)
	ctx = ctx.WithBlockTime(time.Unix(young, 0))
	setStandingDoc(k, ctx, "did:phi:y", ctrl, young, types.DID_STATUS_ACTIVE)

	frozenAt := young + 1_000
	cutoff := time.Unix(young-1, 0) // the young DID does not clear it

	ctx = ctx.WithBlockTime(time.Unix(young+2_000, 0))
	setStandingDoc(k, ctx, "did:phi:y", ctrl, young, types.DID_STATUS_SUSPENDED)
	ctx = ctx.WithBlockTime(time.Unix(young+3_000, 0))
	setStandingDoc(k, ctx, "did:phi:y", ctrl, young, types.DID_STATUS_ACTIVE)

	require.False(t, k.IsEligibleControllerSince(ctx, ctrl, cutoff, 0, time.Unix(frozenAt, 0)),
		"a suspend/reinstate changes no created_at, so a young controller stays outside the basis")
}
