// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

func tierLimits() []types.KycTierLimit {
	return []types.KycTierLimit{
		{Tier: 1, DailyLimitToman: "500"},   // the strictest
		{Tier: 2, DailyLimitToman: "5000"},  // generous
		{Tier: 3, DailyLimitToman: "50000"}, // most generous
		{Tier: kycFundingTier, DailyLimitToman: "1000000000"},
	}
}

const kycFundingTier = uint32(9)

func setupKycTier(t *testing.T, holder sdk.AccAddress) fixture {
	t.Helper()
	f := setupDIDCap(t, "200000000", map[string]string{holder.String(): "did:phi:holder"})
	f.registerAndAttestWithKycTiers(t, "bank-a", 100_000_000, tierLimits())
	return f
}

// THE PIN.
func TestKycTier_UnassignedHolderGetsTheStrictestLimit(t *testing.T) {
	holder := sdk.AccAddress([]byte("holder______________"))
	f := setupKycTier(t, holder)
	f.mintTo(t, "bank-a", holder, "100000", "dep-1")

	require.NoError(t, f.redeem("bank-a", holder, "500", "red-1"))

	require.ErrorIs(t, f.redeem("bank-a", holder, "1", "red-2"), types.ErrKycTierExceeded,
		"an unassigned holder must be held to the strictest configured limit")
}

// A holder cannot raise their own limit by asserting a tier in the transaction they sign — including a tier the institution never configured, which is what used to remove the limit entirely.
func TestKycTier_HolderCannotRaiseTheirOwnTier(t *testing.T) {
	holder := sdk.AccAddress([]byte("holder______________"))

	for _, asserted := range []uint32{0, 1, 2, 3, 99} {
		f := setupKycTier(t, holder)
		f.mintTo(t, "bank-a", holder, "100000", "dep-1")

		_, err := f.msg.InstitutionRedeem(f.ctx, &types.MsgInstitutionRedeem{
			Admin: holder.String(), Institution: "bank-a", Holder: holder.String(),
			AmountToman: "5000", RedeemRef: "red-assert", KycTier: asserted,
		})
		require.ErrorIs(t, err, types.ErrKycTierExceeded,
			"asserting tier %d in the signed transaction must not raise the holder's limit", asserted)
	}
}

// The tier a holder asserts is ignored entirely: the outcome is identical whatever they claim.
func TestKycTier_AssertedTierChangesNothing(t *testing.T) {
	holder := sdk.AccAddress([]byte("holder______________"))

	outcome := func(asserted uint32) error {
		f := setupKycTier(t, holder)
		f.mintTo(t, "bank-a", holder, "100000", "dep-1")
		_, err := f.msg.InstitutionRedeem(f.ctx, &types.MsgInstitutionRedeem{
			Admin: holder.String(), Institution: "bank-a", Holder: holder.String(),
			AmountToman: "501", RedeemRef: "red-1", KycTier: asserted,
		})
		return err
	}

	base := outcome(0)
	for _, asserted := range []uint32{1, 2, 3, 99} {
		got := outcome(asserted)
		require.Equal(t, base != nil, got != nil,
			"the asserted tier must not change the outcome (tier %d)", asserted)
	}
}

// The compliance-set tier is honoured when it is present in state.
func TestKycTier_ComplianceSetTierRaisesTheLimit(t *testing.T) {
	holder := sdk.AccAddress([]byte("holder______________"))
	f := setupKycTier(t, holder)
	f.mintTo(t, "bank-a", holder, "100000", "dep-1")

	require.ErrorIs(t, f.redeem("bank-a", holder, "5000", "red-1"), types.ErrKycTierExceeded)

	f.k.SetHolderKycTier(f.ctx, "bank-a", holder, 2)

	require.NoError(t, f.redeem("bank-a", holder, "5000", "red-2"),
		"a tier set in compliance state must raise the limit")
	require.ErrorIs(t, f.redeem("bank-a", holder, "1", "red-3"), types.ErrKycTierExceeded,
		"and the raised limit still binds")
}

// A tier assigned to a value the institution never configured must fall back to the strictest limit, not to no limit.
func TestKycTier_AssignedButUnconfiguredTierFallsBackToStrictest(t *testing.T) {
	holder := sdk.AccAddress([]byte("holder______________"))
	f := setupKycTier(t, holder)
	f.mintTo(t, "bank-a", holder, "100000", "dep-1")

	f.k.SetHolderKycTier(f.ctx, "bank-a", holder, 99) // no such tier is configured

	require.NoError(t, f.redeem("bank-a", holder, "500", "red-1"))
	require.ErrorIs(t, f.redeem("bank-a", holder, "1", "red-2"), types.ErrKycTierExceeded,
		"an unconfigured tier must fall back to the strictest limit, never to no limit")
}

// A tier is scoped to the institution that assigned it: a holder graded at one institution is not thereby graded at another.
func TestKycTier_TierIsScopedToTheInstitutionThatSetIt(t *testing.T) {
	holder := sdk.AccAddress([]byte("holder______________"))
	f := setupKycTier(t, holder)
	f.registerAndAttestWithKycTiers(t, "bank-b", 100_000_000, tierLimits())
	f.mintTo(t, "bank-a", holder, "100000", "dep-a")
	f.mintTo(t, "bank-b", holder, "100000", "dep-b")

	f.k.SetHolderKycTier(f.ctx, "bank-a", holder, 3)

	require.NoError(t, f.redeem("bank-a", holder, "5000", "red-a"),
		"the grading institution honours its own assignment")
	require.ErrorIs(t, f.redeem("bank-b", holder, "5000", "red-b"), types.ErrKycTierExceeded,
		"another institution must not inherit that assignment")
}

// An institution that configured no KYC tiers has expressed no KYC policy, so no tier limit applies — but the redemption is still bound by the institution's own caps and the network-wide per-human cap.
func TestKycTier_NoConfiguredTiersLeavesOtherCapsInForce(t *testing.T) {
	holder := sdk.AccAddress([]byte("holder______________"))
	f := setupDIDCap(t, "20000", map[string]string{holder.String(): "did:phi:holder"})
	f.registerAndAttest(t, "bank-a", 100_000_000) // no KYC tiers configured
	f.mintTo(t, "bank-a", holder, "100000", "dep-1")

	require.NoError(t, f.redeem("bank-a", holder, "2000", "red-1"))
	require.ErrorIs(t, f.redeem("bank-a", holder, "1", "red-2"), types.ErrCapExceeded,
		"with no KYC policy the other caps must still bound the redemption")
}
