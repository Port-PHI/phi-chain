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

func eqInt(t *testing.T, want, got math.Int, msg string, args ...any) {
	t.Helper()
	require.Truef(t, want.Equal(got), "%s (want %s, got %s)", fmt.Sprintf(msg, args...), want, got)
}

const phiToToman = uint64(100_000) // canonical: k = UphiPerPhi / phi_to_toman = 10 uphi per toman

// The split is exhaustive and lossless: UphiIn == Burned + ProtocolFee + Penalty + Dust, always.
func TestComputeRedeemSplit_IsExhaustiveAndLossless(t *testing.T) {
	for _, tomanIn := range []int64{1, 3, 7, 101, 999, 1_000, 12_345, 1_000_000} {
		uphiIn := math.NewInt(tomanIn * 10)
		for _, feeBps := range []int64{0, 1, 20, 100, 1_000} {
			for _, penBps := range []int64{0, 20, 100} {
				fee := uphiIn.MulRaw(feeBps).QuoRaw(10_000)
				pen := uphiIn.MulRaw(penBps).QuoRaw(10_000)

				s := keeper.ComputeRedeemSplit(uphiIn, fee, pen, phiToToman)

				eqInt(t, uphiIn, s.Burned.Add(s.ProtocolFee).Add(s.Penalty).Add(s.Dust),
					"toman=%d fee=%dbps pen=%dbps: the four buckets must sum to what was surrendered",
					tomanIn, feeBps, penBps)
				eqInt(t, s.Carved, s.ProtocolFee.Add(s.Penalty).Add(s.Dust),
					"the carve-out is exactly fee + penalty + dust")
				eqInt(t, uphiIn, s.Burned.Add(s.Carved), "burned + carved == surrendered")

				require.True(t, s.Burned.ModRaw(10).IsZero(),
					"burned %s must be a multiple of k=10", s.Burned)
				eqInt(t, s.Burned, s.TomanOut.MulRaw(10),
					"toman_out must convert back to the burned amount exactly")

				require.False(t, s.Burned.IsNegative())
				require.False(t, s.Dust.IsNegative(), "dust must never be negative")
				require.True(t, s.Dust.LT(math.NewInt(10)), "dust is bounded by k-1 = 9 uphi")
			}
		}
	}
}

// Dust case: 101 toman = 1,010 uphi at 0.2% fee + 1% penalty; the 8 uphi rounding remainder joins the carve-out.
func TestComputeRedeemSplit_DustJoinsTheCarveOutAndNothingVanishes(t *testing.T) {
	uphiIn := math.NewInt(1_010)
	s := keeper.ComputeRedeemSplit(uphiIn, math.NewInt(2), math.NewInt(10), phiToToman)

	eqInt(t, math.NewInt(990), s.Burned, "the burn is rounded down to a multiple of 10")
	eqInt(t, math.NewInt(2), s.ProtocolFee, "protocol fee")
	eqInt(t, math.NewInt(10), s.Penalty, "penalty")
	eqInt(t, math.NewInt(8), s.Dust, "998 - 990 = 8 uphi of rounding remainder")
	eqInt(t, math.NewInt(20), s.Carved, "2 + 10 + 8")
	eqInt(t, math.NewInt(99), s.TomanOut, "990 uphi = 99 toman, exactly")

	eqInt(t, uphiIn, s.Burned.Add(s.Carved), "nothing vanished")
}

// Zero fee and zero penalty: everything is burned, nothing carved.
func TestComputeRedeemSplit_NoFeeNoPenaltyBurnsEverything(t *testing.T) {
	s := keeper.ComputeRedeemSplit(math.NewInt(100_000), math.ZeroInt(), math.ZeroInt(), phiToToman)

	eqInt(t, math.NewInt(100_000), s.Burned, "everything is burned")
	require.True(t, s.Carved.IsZero())
	require.True(t, s.Dust.IsZero())
	eqInt(t, math.NewInt(10_000), s.TomanOut, "toman out")
}

// Absurd inputs still produce a coherent split (carve clamped, minimum-burn floor pays one toman).
func TestComputeRedeemSplit_ClampsAnOverlargeCarve(t *testing.T) {
	uphiIn := math.NewInt(1_000)
	s := keeper.ComputeRedeemSplit(uphiIn, math.NewInt(900), math.NewInt(900), phiToToman)

	eqInt(t, keeper.UphiPerToman(phiToToman), s.Burned, "the floor still pays out one toman")
	eqInt(t, uphiIn.Sub(s.Burned), s.Carved, "everything else is carved")
	eqInt(t, uphiIn, s.Burned.Add(s.ProtocolFee).Add(s.Penalty).Add(s.Dust),
		"even clamped, the split must still sum to what was surrendered")
	require.True(t, s.TomanOut.IsPositive())
}

// k is derived: at a different phi_to_toman the burn rounds to that k.
func TestUphiPerToman_DerivesKFromTheRate(t *testing.T) {
	eqInt(t, math.NewInt(10), keeper.UphiPerToman(100_000), "canonical: 10 uphi per toman")
	eqInt(t, math.NewInt(2), keeper.UphiPerToman(500_000), "k at phi_to_toman=500,000")

	s := keeper.ComputeRedeemSplit(math.NewInt(1_000), math.NewInt(3), math.ZeroInt(), 500_000)
	eqInt(t, math.NewInt(996), s.Burned, "1000 - 3 = 997 -> rounded down to 996 (even)")
	eqInt(t, math.NewInt(498), s.TomanOut, "toman out")
	eqInt(t, math.NewInt(1), s.Dust, "dust")
	eqInt(t, math.NewInt(1_000), s.Burned.Add(s.Carved), "nothing vanished")
}

// Max fee (x/institutions) against max penalty (x/coin) at once must still leave value to burn; x/coin's MaxProtocolFeeReserveBps reserves the headroom for the fee.
func TestComputeRedeemSplit_MaxPenaltyAndMaxFeeStillBurn(t *testing.T) {
	uphiIn := math.NewInt(1_000_000_000)

	maxFeeParams := insttypes.DefaultParams()
	maxFeeParams.ProtocolFeeBps = insttypes.MaxProtocolFeeBps
	fee := maxFeeParams.ProtocolFee(uphiIn)

	maxPenaltyBps := int64(cointypes.BpsDenominator - cointypes.MaxProtocolFeeReserveBps - 1)
	penalty := uphiIn.MulRaw(maxPenaltyBps).QuoRaw(int64(cointypes.BpsDenominator))

	s := keeper.ComputeRedeemSplit(uphiIn, fee, penalty, phiToToman)

	require.True(t, s.Burned.IsPositive(),
		"max penalty + max protocol fee must still leave value to burn (got burned=%s of %s)", s.Burned, uphiIn)
	require.True(t, s.TomanOut.IsPositive(), "and the holder must be paid something")
	eqInt(t, uphiIn, s.Burned.Add(s.ProtocolFee).Add(s.Penalty).Add(s.Dust), "the split still sums to what was surrendered")

	overPenalty := uphiIn.MulRaw(int64(cointypes.BpsDenominator - cointypes.MaxProtocolFeeReserveBps)).QuoRaw(int64(cointypes.BpsDenominator))
	over := keeper.ComputeRedeemSplit(uphiIn, fee, overPenalty, phiToToman)
	require.True(t, over.Burned.IsPositive(), "a redemption must return value even at a 100% carve rate")
	require.Equal(t, keeper.UphiPerToman(phiToToman), over.Burned,
		"and at that rate it returns exactly the minimum: the floor, not the economics")

	p := cointypes.DefaultParams()
	p.YoungPenaltyBps = uint32(cointypes.BpsDenominator - cointypes.MaxProtocolFeeReserveBps)
	p.OldPenaltyBps = 0
	require.Error(t, p.Validate(), "penalties that consume the fee's reserve must not validate")
}
