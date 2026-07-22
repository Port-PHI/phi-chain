// SPDX-License-Identifier: Apache-2.0

package types

import (
	"fmt"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
)

func ageWeight(lots []CoinLot) math.Int {
	total := math.ZeroInt()
	for _, l := range lots {
		total = total.Add(LotAmount(l).MulRaw(l.AcquiredAt))
	}
	return total
}

// THE PIN.
func TestBoundLots_NeverManufacturesAge(t *testing.T) {
	now := int64(1_700_000_000)

	for _, bound := range []uint32{2, 3, 5, 16, 64} {
		t.Run(fmt.Sprintf("bound_%d", bound), func(t *testing.T) {
			var q []CoinLot
			for i := 0; i < 200; i++ {
				lot := CoinLot{
					Amount:     math.NewInt(int64(1 + (i*7)%50)).String(),
					AcquiredAt: now - int64(200-i)*day,
				}

				before := ageWeight(append(append([]CoinLot{}, q...), lot))
				q = InsertLot(q, lot, bound)
				after := ageWeight(q)

				require.True(t, after.GTE(before),
					"step %d: bounding the queue reported it as older than it is (%s -> %s)",
					i, before, after)
				require.LessOrEqual(t, uint32(len(q)), bound)
			}
		})
	}
}

// A merged lot's timestamp must lie between the two it replaces: a merge can neither invent age nor destroy it.
func TestBoundLots_MergedTimestampLiesBetweenItsInputs(t *testing.T) {
	now := int64(1_700_000_000)

	for _, tc := range []struct {
		name             string
		olderAmt, newAmt int64
		olderAt, newAt   int64
	}{
		{"equal amounts", 100, 100, now - 100, now},
		{"older dominates", 1_000_000, 1, now - 1000, now},
		{"newer dominates", 1, 1_000_000, now - 1000, now},
		{"same timestamp", 5, 7, now - 50, now - 50},
		{"zero amounts", 0, 0, now - 50, now},
	} {
		t.Run(tc.name, func(t *testing.T) {
			older := CoinLot{Amount: math.NewInt(tc.olderAmt).String(), AcquiredAt: tc.olderAt}
			newer := CoinLot{Amount: math.NewInt(tc.newAmt).String(), AcquiredAt: tc.newAt}

			got := mergedAcquiredAt(older, newer)
			require.GreaterOrEqual(t, got, tc.olderAt, "a merge must not report coin as older than its oldest part")
			require.LessOrEqual(t, got, tc.newAt, "a merge must not report coin as younger than its youngest part")
		})
	}
}

// A holder churning coin through their own queue must not end up paying less than they owe.
func TestBoundLots_ChurnCannotLowerAHoldersOwnPenalty(t *testing.T) {
	now := int64(1_700_000_000)
	p := DefaultParams()
	const bound = uint32(4)

	q := []CoinLot{{Amount: "1", AcquiredAt: now - 365*day}}

	const churns = 500
	const perLot = 1_000
	for i := 0; i < churns; i++ {
		q = InsertLot(q, CoinLot{Amount: math.NewInt(perLot).String(), AcquiredAt: now}, bound)
	}

	fresh := math.NewInt(churns * perLot)
	consumed, _ := SpendOldestFirst(q, fresh, now, p.CoinAgeThresholdSeconds)
	got := PenaltyForLots(consumed, now, p)

	owed := fresh.MulRaw(int64(p.YoungPenaltyBps)).QuoRaw(BpsDenominator)

	require.True(t, got.GTE(owed.MulRaw(99).QuoRaw(100)),
		"churning coin through the queue lowered the holder's own penalty: paid %s, owed about %s", got, owed)
}

// The other direction, stated plainly: a holder cannot lower their penalty by splitting a holding into many lots and merging them back down.
func TestBoundLots_SplittingAndMergingIsPenaltyNeutral(t *testing.T) {
	now := int64(1_700_000_000)
	p := DefaultParams()
	const total = 100_000

	single := []CoinLot{{Amount: math.NewInt(total).String(), AcquiredAt: now}}
	consumedSingle, _ := SpendOldestFirst(single, math.NewInt(total), now, p.CoinAgeThresholdSeconds)
	penaltySingle := PenaltyForLots(consumedSingle, now, p)

	var split []CoinLot
	for i := 0; i < 100; i++ {
		split = InsertLot(split, CoinLot{Amount: math.NewInt(total / 100).String(), AcquiredAt: now}, 4)
	}
	consumedSplit, _ := SpendOldestFirst(split, math.NewInt(total), now, p.CoinAgeThresholdSeconds)
	penaltySplit := PenaltyForLots(consumedSplit, now, p)

	require.Equal(t, penaltySingle.String(), penaltySplit.String(),
		"splitting a holding and merging it back must not change what it owes")
}

// Merging must still conserve the amount exactly — the weighting changes only the timestamp.
func TestBoundLots_WeightingConservesAmount(t *testing.T) {
	now := int64(1_700_000_000)
	var q []CoinLot
	total := math.ZeroInt()
	for i := 0; i < 100; i++ {
		amt := math.NewInt(int64(i + 1))
		total = total.Add(amt)
		q = InsertLot(q, CoinLot{Amount: amt.String(), AcquiredAt: now - int64(100-i)*day}, 5)
	}
	require.Equal(t, total.String(), TotalLots(q).String(), "merging conserves the amount exactly")
}
