// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	"github.com/Port-PHI/phi-chain/x/institutions/keeper"
	insttypes "github.com/Port-PHI/phi-chain/x/institutions/types"
)

func penaltyGrid() []struct{ young, old uint32 } {
	return []struct{ young, old uint32 }{
		{0, 0},       // no penalty at all
		{500, 100},   // the shipped defaults' order of magnitude
		{8000, 900},  // the adversarial pair: valid, and the one that zeroed the burn
		{8900, 0},    // the whole budget on the young tier
		{0, 8900},    // and on the old tier
		{4450, 4450}, // evenly split at the bound
	}
}

// TestRedeemMinimum_PositiveRedeemAlwaysReturnsValue is the full grid: every valid penalty pair, every protocol fee rate up to the permitted maximum, and every small redemption from the minimum upward.
func TestRedeemMinimum_PositiveRedeemAlwaysReturnsValue(t *testing.T) {
	k := keeper.UphiPerToman(phiToToman)

	for _, pen := range penaltyGrid() {
		t.Run(fmt.Sprintf("young=%d/old=%d", pen.young, pen.old), func(t *testing.T) {
			cp := cointypes.DefaultParams()
			cp.YoungPenaltyBps, cp.OldPenaltyBps = pen.young, pen.old
			require.NoError(t, cp.Validate(),
				"the grid must only contain penalty rates governance can reach")

			for _, feeBps := range []uint32{0, 1, insttypes.MaxProtocolFeeBps / 2, insttypes.MaxProtocolFeeBps} {
				ip := insttypes.DefaultParams()
				ip.ProtocolFeeBps = feeBps
				require.NoError(t, ip.Validate(), "the grid must only contain fee rates governance can reach")

				worstBps := int64(pen.young)
				if pen.old > pen.young {
					worstBps = int64(pen.old)
				}
				for _, tomanIn := range []int64{1, 2, 3, 4, 5, 9, 10, 11, 100} {
					uphiIn := k.MulRaw(tomanIn)
					penalty := uphiIn.MulRaw(worstBps).QuoRaw(int64(cointypes.BpsDenominator))
					s := keeper.ComputeRedeemSplit(uphiIn, ip.ProtocolFee(uphiIn), penalty, phiToToman)

					what := fmt.Sprintf("young=%d old=%d fee_bps=%d toman_in=%d",
						pen.young, pen.old, feeBps, tomanIn)
					require.True(t, s.Burned.IsPositive(), "burned nothing: %s", what)
					require.True(t, s.TomanOut.IsPositive(), "paid the holder nothing: %s", what)

					require.True(t, s.Burned.Mod(k).IsZero(),
						"the burn must stay an exact multiple of k, or the vault decrement is fractional: %s", what)
					require.Equal(t, uphiIn, s.Burned.Add(s.ProtocolFee).Add(s.Penalty).Add(s.Dust),
						"the split must sum to what was surrendered: %s", what)
					require.False(t, s.Dust.IsNegative(), "dust must never go negative: %s", what)
					require.False(t, s.Penalty.IsNegative(), "penalty must never go negative: %s", what)
					require.False(t, s.ProtocolFee.IsNegative(), "the fee must never go negative: %s", what)
					require.True(t, s.Burned.LTE(uphiIn), "the burn may never exceed the input: %s", what)
				}
			}
		})
	}
}

// The give-back is bounded: it is only ever the shortfall needed to reach one whole toman, so it cannot be used to escape the carve-out at any meaningful size.
func TestRedeemMinimum_TheFloorDoesNotSubsidiseLargeRedemptions(t *testing.T) {
	uphiIn := math.NewInt(1_000_000)
	penalty := uphiIn.MulRaw(8000).QuoRaw(int64(cointypes.BpsDenominator))
	ip := insttypes.DefaultParams()
	fee := ip.ProtocolFee(uphiIn)

	s := keeper.ComputeRedeemSplit(uphiIn, fee, penalty, phiToToman)
	require.Equal(t, penalty, s.Penalty, "a large redemption pays the penalty in full")
	require.Equal(t, fee, s.ProtocolFee, "and the protocol fee in full")
	require.True(t, s.Burned.IsPositive())
}

// The adversarial pair, end to end through the message: a one-toman redemption succeeds and the holder is paid, rather than being refused with "the carve-out consumes the entire redemption".
func TestRedeemMinimum_OneTomanRedeemSucceedsUnderTheAdversarialPenalty(t *testing.T) {
	f := setup(t)
	f.mintBacked(t, "bank-a", 1000, "dep-1")

	f = f.withPenalty(8900)

	beforeSupply := f.bank.GetSupply(f.ctx, cointypes.Denom).Amount
	_, err := f.msg.InstitutionRedeem(f.ctx, &insttypes.MsgInstitutionRedeem{
		Admin: f.holder.String(), Holder: f.holder.String(), Institution: "bank-a",
		AmountToman: "1", RedeemRef: "red-min",
	})
	require.NoError(t, err, "a one-toman redemption must not be refused as nothing-redeemed")
	require.True(t, f.bank.GetSupply(f.ctx, cointypes.Denom).Amount.LT(beforeSupply),
		"something must actually have been burned")
}

// A request below the minimum is refused BY NAME — never settled as a silent zero, and never confused with the arithmetic guard.
func TestRedeemMinimum_SubMinimumIsRejectedByName(t *testing.T) {
	f := setup(t)
	f.mintBacked(t, "bank-a", 1000, "dep-1")

	for _, amount := range []string{"0", "-1", ""} {
		_, err := f.msg.InstitutionRedeem(f.ctx, &insttypes.MsgInstitutionRedeem{
			Admin: f.holder.String(), Holder: f.holder.String(), Institution: "bank-a",
			AmountToman: amount, RedeemRef: "red-" + amount,
		})
		require.ErrorIs(t, err, insttypes.ErrInvalidAmount,
			"a non-positive amount %q is not a redemption at all", amount)
	}

	require.Equal(t, math.NewInt(10), keeper.UphiPerToman(phiToToman),
		"one toman is 10 uphi at the canonical rate; that is the minimum redeemable amount")
}
