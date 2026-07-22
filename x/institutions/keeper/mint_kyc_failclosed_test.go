// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

func mintKycFixture(t *testing.T) (fixture, sdk.AccAddress) {
	t.Helper()
	f := setup(t)
	f.registerAndAttestWithKycTiers(t, "bank-a", 100_000_000, []types.KycTierLimit{
		{Tier: 1, DailyLimitToman: "500"},   // the strictest
		{Tier: 2, DailyLimitToman: "5000"},  //
		{Tier: 3, DailyLimitToman: "50000"}, // the most generous
	})
	return f, sdk.AccAddress([]byte("mint-kyc-holder_____"))
}

func (f fixture) mintTier(inst string, to sdk.AccAddress, toman string, tier uint32, ref string) error {
	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: inst, Recipient: to.String(),
		AmountToman: toman, KycTier: tier, DepositRef: ref,
	})
	return err
}

// Every unconfigured tier a mint can assert is bound by the strictest configured limit.
func TestMintKyc_AnUnconfiguredTierFallsToTheStrictestLimit(t *testing.T) {
	for _, tier := range []uint32{0, 4, 7, 99, 4_000_000_000} {
		t.Run("tier", func(t *testing.T) {
			f, holder := mintKycFixture(t)

			require.NoError(t, f.mintTier("bank-a", holder, "500", tier, "dep-a"))
			require.ErrorIs(t, f.mintTier("bank-a", holder, "1", tier, "dep-b"),
				types.ErrKycTierExceeded,
				"an unconfigured tier %d must fall to the strictest limit, not escape the cap", tier)
		})
	}
}

// A CONFIGURED tier applies as configured.
func TestMintKyc_AConfiguredTierApplies(t *testing.T) {
	f, holder := mintKycFixture(t)

	require.NoError(t, f.mintTier("bank-a", holder, "50000", 3, "dep-1"))
	require.ErrorIs(t, f.mintTier("bank-a", holder, "1", 3, "dep-2"), types.ErrKycTierExceeded,
		"and the configured tier still binds at its own limit")
}

// The STRICTER of the recorded (compliance-gated) tier and the asserted tier wins.
func TestMintKyc_ARecordedStricterTierOverridesTheAssertedOne(t *testing.T) {
	f, holder := mintKycFixture(t)

	f.k.SetHolderKycTier(f.ctx, "bank-a", holder, 1) // records tier 1 (500/day)

	require.NoError(t, f.mintTier("bank-a", holder, "500", 3, "dep-1"))
	require.ErrorIs(t, f.mintTier("bank-a", holder, "1", 3, "dep-2"), types.ErrKycTierExceeded,
		"a stricter recorded tier must not be widened by the tier asserted in the message")
}

// A recorded tier MORE generous than the asserted one does not widen it.
func TestMintKyc_ARecordedLooserTierDoesNotWidenTheAssertedOne(t *testing.T) {
	f, holder := mintKycFixture(t)
	f.k.SetHolderKycTier(f.ctx, "bank-a", holder, 3) // 50,000/day recorded

	require.NoError(t, f.mintTier("bank-a", holder, "500", 1, "dep-1")) // asserted tier 1: 500/day
	require.ErrorIs(t, f.mintTier("bank-a", holder, "1", 1, "dep-2"), types.ErrKycTierExceeded,
		"the stricter of the asserted and recorded tiers binds")
}

// An institution with NO tier policy has no limit to fall back to.
func TestMintKyc_NoTierPolicyMeansNoTierLimit(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 100_000_000)
	holder := sdk.AccAddress([]byte("mint-kyc-holder_____"))

	require.NoError(t, f.mintTier("bank-a", holder, "1000000", 42, "dep-1"),
		"with no KYC policy configured there is no tier limit to fall closed onto")
}

// The signer is the institution: a holder cannot mint to themselves under a tier they chose.
func TestMintKyc_TheHolderCannotAssertTheirOwnTier(t *testing.T) {
	f, holder := mintKycFixture(t)

	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: holder.String(), Institution: "bank-a", Recipient: holder.String(),
		AmountToman: "50000", KycTier: 3, DepositRef: "dep-self",
	})
	require.Error(t, err, "a holder must not be able to mint to themselves under a tier they chose")
}
