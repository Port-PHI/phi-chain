// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

func eligibilityFixture(t *testing.T, k Keeper, ctx sdk.Context, did, controller string, createdAt int64) {
	t.Helper()
	k.SetIdentity(ctx, types.DIDDocument{
		Did:        did,
		Controller: controller,
		Status:     types.DID_STATUS_ACTIVE,
		CreatedAt:  createdAt,
	})
}

// TestEligibleSince_SuspendReinstateKeepsAFrozenDenominatorMember is the suspend/reinstate fix: a controller counted in a frozen denominator, suspended and then reinstated during the voting period, is STILL admitted against that frozen basis.
func TestEligibleSince_SuspendReinstateKeepsAFrozenDenominatorMember(t *testing.T) {
	ctx, k := setupInternal(t)
	const controller = "phi1controller"

	born := int64(1_000_000)
	ctx = ctx.WithBlockTime(time.Unix(born, 0))
	eligibilityFixture(t, k, ctx, "did:phi:1", controller, born)
	require.Equal(t, uint64(1), k.EligibleControllerTotal(ctx))

	frozenAt := born + 1_000
	cutoff := time.Unix(frozenAt, 0)
	require.Equal(t, uint64(1), k.CountEligibleControllersAt(ctx, cutoff, 0))
	require.True(t, k.IsEligibleControllerSince(ctx, controller, cutoff, 0, time.Unix(frozenAt, 0)),
		"the controller is in the frozen denominator")

	suspendedAt := frozenAt + 1_000
	ctx = ctx.WithBlockTime(time.Unix(suspendedAt, 0))
	k.SetIdentity(ctx, types.DIDDocument{
		Did: "did:phi:1", Controller: controller,
		Status: types.DID_STATUS_SUSPENDED, CreatedAt: born,
	})
	require.Equal(t, uint64(1), k.CountEligibleControllersAt(ctx, cutoff, 0),
		"a suspended controller stays counted in the denominator")

	reinstatedAt := suspendedAt + 1_000
	ctx = ctx.WithBlockTime(time.Unix(reinstatedAt, 0))
	eligibilityFixture(t, k, ctx, "did:phi:1", controller, born)

	require.True(t, k.IsEligibleControllerSince(ctx, controller, cutoff, 0, time.Unix(frozenAt, 0)),
		"a controller counted at the freeze must still vote after a suspend/reinstate round trip")
}

// Continuous eligibility must survive writes that do not change it — a key rotation, a second DID, or re-setting an identity unchanged.
func TestEligibleSince_ContinuousEligibilityIsNotRestartedByUnrelatedWrites(t *testing.T) {
	ctx, k := setupInternal(t)
	const controller = "phi1steady"

	born := int64(2_000_000)
	ctx = ctx.WithBlockTime(time.Unix(born, 0))
	eligibilityFixture(t, k, ctx, "did:phi:a", controller, born)
	k.refreshControllerEligibility(ctx, controller)

	frozenAt := born + 500

	for i, step := range []func(ctx sdk.Context){
		// Re-setting the same identity unchanged.
		func(ctx sdk.Context) { eligibilityFixture(t, k, ctx, "did:phi:a", controller, born) },
		// A second, NEWER DID: the oldest ACTIVE DID does not move.
		func(ctx sdk.Context) { eligibilityFixture(t, k, ctx, "did:phi:b", controller, born+2_000) },
		// Revoking the newer one again.
		func(ctx sdk.Context) {
			k.SetIdentity(ctx, types.DIDDocument{
				Did: "did:phi:b", Controller: controller,
				Status: types.DID_STATUS_REVOKED, CreatedAt: born + 2_000,
			})
		},
	} {
		ctx = ctx.WithBlockTime(time.Unix(born+10_000+int64(i)*1_000, 0))
		step(ctx)
		k.refreshControllerEligibility(ctx, controller)

		require.True(t, k.IsEligibleControllerSince(ctx, controller,
			time.Unix(frozenAt, 0), 0, time.Unix(frozenAt, 0)),
			"step %d must not restart continuous eligibility", i)
	}
}

// A record written before eligible_since existed decodes as eligible from before any basis, so an existing registry is not disenfranchised by the field's introduction.
func TestEligibleSince_LegacyRecordsKeepTheirMeaning(t *testing.T) {
	oldest, since, ok := types.DecodeControllerEligibility(types.SortableInt64(1_234))
	require.True(t, ok)
	require.Equal(t, int64(1_234), oldest)
	require.Zero(t, since)

	oldest, since, ok = types.DecodeControllerEligibility(types.EncodeControllerEligibility(7, 9))
	require.True(t, ok)
	require.Equal(t, int64(7), oldest)
	require.Equal(t, int64(9), since)

	_, _, ok = types.DecodeControllerEligibility(nil)
	require.False(t, ok)
	_, _, ok = types.DecodeControllerEligibility([]byte{1, 2, 3})
	require.False(t, ok, "a malformed record must never read as a plausible timestamp")
}

// The oldest ACTIVE created_at was assumed never to move earlier, and social recovery is the counterexample: it hands an EXISTING DID, with its original creation time, to a controller that may already hold a younger one.
func TestEligibleSince_AnOlderDIDArrivingRestartsEligibility(t *testing.T) {
	ctx, k := setupInternal(t)
	const controller = "phi1recovered"

	young := int64(5_000_000)
	ctx = ctx.WithBlockTime(time.Unix(young, 0))
	eligibilityFixture(t, k, ctx, "did:phi:young", controller, young)
	k.refreshControllerEligibility(ctx, controller)

	frozenAt := young + 1_000
	cutoff := time.Unix(young-1, 0)
	require.False(t, k.IsEligibleControllerSince(ctx, controller, cutoff, 0, time.Unix(frozenAt, 0)),
		"the young DID is outside the frozen denominator")

	recoveredAt := frozenAt + 1_000
	ctx = ctx.WithBlockTime(time.Unix(recoveredAt, 0))
	eligibilityFixture(t, k, ctx, "did:phi:old", controller, young-100_000)
	k.refreshControllerEligibility(ctx, controller)

	require.True(t, k.IsEligibleControllerAt(ctx, controller, cutoff, 0),
		"the recovered DID genuinely predates the cutoff")

	require.False(t, k.IsEligibleControllerSince(ctx, controller, cutoff, 0, time.Unix(frozenAt, 0)),
		"an eligibility basis that improved after the freeze cannot be claimed against it")

	require.True(t, k.IsEligibleControllerSince(ctx, controller, cutoff, 0, time.Unix(recoveredAt+1, 0)))
}

// Losing the oldest DID moves the basis LATER, which can only ever shrink what the controller is admitted to.
func TestEligibleSince_LosingTheOldestDIDDoesNotRestartEligibility(t *testing.T) {
	ctx, k := setupInternal(t)
	const controller = "phi1shrinking"

	born := int64(6_000_000)
	ctx = ctx.WithBlockTime(time.Unix(born, 0))
	eligibilityFixture(t, k, ctx, "did:phi:first", controller, born)
	eligibilityFixture(t, k, ctx, "did:phi:second", controller, born+1_000)
	k.refreshControllerEligibility(ctx, controller)

	frozenAt := born + 2_000
	cutoff := time.Unix(born+5_000, 0)

	ctx = ctx.WithBlockTime(time.Unix(frozenAt+1_000, 0))
	k.SetIdentity(ctx, types.DIDDocument{
		Did: "did:phi:first", Controller: controller,
		Status: types.DID_STATUS_REVOKED, CreatedAt: born,
	})
	k.refreshControllerEligibility(ctx, controller)

	require.True(t, k.IsEligibleControllerSince(ctx, controller, cutoff, 0, time.Unix(frozenAt, 0)),
		"a basis that only got worse leaves continuous eligibility intact")
}
