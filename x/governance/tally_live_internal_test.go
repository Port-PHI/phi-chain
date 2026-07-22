// SPDX-License-Identifier: Apache-2.0

package governance

import (
	"testing"

	"cosmossdk.io/math"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"
)

func wopt(o v1.VoteOption, w string) *v1.WeightedVoteOption {
	return &v1.WeightedVoteOption{Option: o, Weight: w}
}

func single(o v1.VoteOption) []*v1.WeightedVoteOption {
	return []*v1.WeightedVoteOption{wopt(o, "1.000000000000000000")}
}

func TestDominantOption(t *testing.T) {
	require.Equal(t, v1.OptionYes, dominantOption(single(v1.OptionYes)))
	require.Equal(t, v1.OptionNo, dominantOption(single(v1.OptionNo)))
	require.Equal(t, v1.OptionAbstain, dominantOption(nil))
	require.Equal(t, v1.OptionYes, dominantOption([]*v1.WeightedVoteOption{
		wopt(v1.OptionYes, "0.700000000000000000"), wopt(v1.OptionNo, "0.300000000000000000"),
	}))
	require.Equal(t, v1.OptionAbstain, dominantOption([]*v1.WeightedVoteOption{
		wopt(v1.OptionYes, "0.500000000000000000"), wopt(v1.OptionNo, "0.500000000000000000"),
	}))
}

func govDecision(totalPower math.LegacyDec, results map[v1.VoteOption]math.LegacyDec, totalBonded math.Int, quorum, threshold, veto string) bool {
	if totalBonded.IsZero() || totalPower.IsZero() {
		return false
	}
	q := math.LegacyMustNewDecFromStr(quorum)
	th := math.LegacyMustNewDecFromStr(threshold)
	vt := math.LegacyMustNewDecFromStr(veto)
	if totalPower.Quo(math.LegacyNewDecFromInt(totalBonded)).LT(q) {
		return false // quorum not reached
	}
	if totalPower.Sub(results[v1.OptionAbstain]).IsZero() {
		return false
	}
	if results[v1.OptionNoWithVeto].Quo(totalPower).GT(vt) {
		return false // veto
	}
	nonAbstain := totalPower.Sub(results[v1.OptionAbstain])
	return results[v1.OptionYes].Quo(nonAbstain).GT(th)
}

func TestScalePublicResults_QuorumIsPerHead(t *testing.T) {
	totalBonded := math.NewInt(1_000_000)
	counts := map[v1.VoteOption]uint64{v1.OptionYes: 200}
	power, results := scalePublicResults(counts, 200, 1000, totalBonded)
	require.False(t, govDecision(power, results, totalBonded, "0.334", "0.500", "0.334"),
		"20% turnout must be below quorum — quorum must be per-head, not money-weighted")

	counts = map[v1.VoteOption]uint64{v1.OptionYes: 450, v1.OptionNo: 100, v1.OptionAbstain: 50}
	power, results = scalePublicResults(counts, 600, 1000, totalBonded)
	require.True(t, govDecision(power, results, totalBonded, "0.334", "0.500", "0.334"))
}

func TestScalePublicResults_StakeIrrelevant(t *testing.T) {
	counts := map[v1.VoteOption]uint64{v1.OptionYes: 7, v1.OptionNo: 3}

	pSmall, rSmall := scalePublicResults(counts, 10, 10, math.NewInt(50))
	dSmall := govDecision(pSmall, rSmall, math.NewInt(50), "0.334", "0.500", "0.334")

	pHuge, rHuge := scalePublicResults(counts, 10, 10, math.NewInt(999_999_999))
	dHuge := govDecision(pHuge, rHuge, math.NewInt(999_999_999), "0.334", "0.500", "0.334")

	require.Equal(t, dSmall, dHuge)
	require.True(t, dSmall, "100% turnout and 70% yes -> passes, independent of stake amount")
}

func TestTallyValidatorBallots_StakeWeighted(t *testing.T) {
	ballots := []valBallot{
		{bonded: math.NewInt(30), options: single(v1.OptionYes)},
		{bonded: math.NewInt(20), options: single(v1.OptionYes)},
		{bonded: math.NewInt(50), options: single(v1.OptionNo)},
	}
	power, results, err := tallyValidatorBallots(ballots)
	require.NoError(t, err)
	require.Equal(t, math.LegacyNewDec(100), power)
	require.Equal(t, math.LegacyNewDec(50), results[v1.OptionYes])
	require.Equal(t, math.LegacyNewDec(50), results[v1.OptionNo])

	split := []valBallot{{bonded: math.NewInt(100), options: []*v1.WeightedVoteOption{
		wopt(v1.OptionYes, "0.600000000000000000"), wopt(v1.OptionNo, "0.400000000000000000"),
	}}}
	power, results, err = tallyValidatorBallots(split)
	require.NoError(t, err)
	require.Equal(t, math.LegacyNewDec(100), power)
	require.Equal(t, math.LegacyNewDec(60), results[v1.OptionYes])
	require.Equal(t, math.LegacyNewDec(40), results[v1.OptionNo])
}

func TestSumBonded(t *testing.T) {
	vals := map[string]v1.ValidatorGovInfo{
		"a": {BondedTokens: math.NewInt(30)},
		"b": {BondedTokens: math.NewInt(70)},
	}
	require.Equal(t, math.NewInt(100), sumBonded(vals))
}
