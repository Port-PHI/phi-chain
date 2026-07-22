// SPDX-License-Identifier: Apache-2.0

package types

import (
	"fmt"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
)

const day = int64(24 * 60 * 60)

func lotsAged(now int64, spec ...[2]int64) []CoinLot {
	out := make([]CoinLot, 0, len(spec))
	for _, s := range spec {
		out = append(out, CoinLot{Amount: math.NewInt(s[0]).String(), AcquiredAt: now - s[1]*day})
	}
	return out
}

// Coin is consumed OLDEST FIRST, and a partially consumed lot is split with its age intact on both halves — the remainder stays the oldest coin the holder has.
func TestSpendOldestFirst_ConsumesTheOldestCoinAndSplitsCleanly(t *testing.T) {
	now := int64(1_000_000_000)
	q := lotsAged(now, [2]int64{100, 10}, [2]int64{100, 1})

	consumed, remaining := SpendOldestFirst(q, math.NewInt(40), now, 7*day)
	require.Len(t, consumed, 1)
	require.Equal(t, "40", consumed[0].Amount)
	require.Equal(t, now-10*day, consumed[0].AcquiredAt, "the OLDEST coin is spent first")

	require.Len(t, remaining, 2)
	require.Equal(t, "60", remaining[0].Amount, "the split remainder keeps its age and stays at the front")
	require.Equal(t, now-10*day, remaining[0].AcquiredAt)
	require.Equal(t, "100", remaining[1].Amount)
	require.Equal(t, now-1*day, remaining[1].AcquiredAt)

	consumed, remaining = SpendOldestFirst(q, math.NewInt(150), now, 7*day)
	require.Len(t, consumed, 2)
	require.Equal(t, "100", consumed[0].Amount)
	require.Equal(t, now-10*day, consumed[0].AcquiredAt)
	require.Equal(t, "50", consumed[1].Amount)
	require.Equal(t, now-1*day, consumed[1].AcquiredAt)
	require.Len(t, remaining, 1)
	require.Equal(t, "50", remaining[0].Amount)

	require.Equal(t, TotalLots(q).String(), TotalLots(consumed).Add(TotalLots(remaining)).String())
}

// An untracked surplus (coin that reached the holder outside this module) is priced as OLD — the lowest tier.
func TestSpendOldestFirst_UntrackedSurplusIsOld(t *testing.T) {
	now := int64(1_000_000_000)
	q := lotsAged(now, [2]int64{100, 1}) // only 100 tracked, all young

	consumed, remaining := SpendOldestFirst(q, math.NewInt(300), now, 7*day)
	require.Empty(t, remaining)
	require.Len(t, consumed, 2)
	require.Equal(t, "100", consumed[0].Amount, "the tracked young lot goes first (it is the oldest)")
	require.Equal(t, "200", consumed[1].Amount, "the 200 untracked uphi")
	require.True(t, IsOld(consumed[1].AcquiredAt, now, 7*day), "untracked coin is priced as OLD")
}

// The penalty is per-lot, at each lot's OWN tier — not a blended proportional rate over the whole balance.
func TestPenaltyForLots_PricesEachLotAtItsOwnTier(t *testing.T) {
	now := int64(1_000_000_000)
	p := DefaultParams() // 7-day threshold, 1% young, 0.2% old

	consumed := lotsAged(now, [2]int64{100_000, 10}, [2]int64{50_000, 1})
	got := PenaltyForLots(consumed, now, p)

	require.Equal(t, "700", got.String())

	onlyOld := lotsAged(now, [2]int64{100_000, 10})
	require.Equal(t, "200", PenaltyForLots(onlyOld, now, p).String(), "aged coin alone pays 0.2%")

	onlyYoung := lotsAged(now, [2]int64{100_000, 1})
	require.Equal(t, "1000", PenaltyForLots(onlyYoung, now, p).String(), "fresh coin alone pays 1%")
}

// The tier boundary is exactly the threshold: at 7 days the coin is already OLD.
func TestIsOld_BoundaryIsInclusive(t *testing.T) {
	now := int64(1_000_000_000)
	require.False(t, IsOld(now-7*day+1, now, 7*day), "one second short of 7 days is still young")
	require.True(t, IsOld(now-7*day, now, 7*day), "exactly 7 days old is OLD")
	require.True(t, IsOld(now-30*day, now, 7*day))
}

// A lot arriving with an OLD timestamp (a transfer of aged coin) lands at its rightful place near the front, not at the tail — otherwise the queue would stop being oldest-first and "spend the oldest" would silently spend the wrong coin.
func TestInsertLot_KeepsTheQueueOldestFirst(t *testing.T) {
	now := int64(1_000_000_000)
	q := lotsAged(now, [2]int64{100, 5}, [2]int64{100, 1})

	q = InsertLot(q, CoinLot{Amount: "50", AcquiredAt: now - 20*day}, 64)
	require.Equal(t, now-20*day, q[0].AcquiredAt, "the 20-day-old lot must sort to the FRONT")
	require.Equal(t, "50", q[0].Amount)

	for i := 1; i < len(q); i++ {
		require.LessOrEqual(t, q[i-1].AcquiredAt, q[i].AcquiredAt, "the queue must stay oldest-first")
	}
}

// ANTI-DILUTION.
func TestBoundLots_MergesAtTheOldEndAndNeverDilutesAgedCoin(t *testing.T) {
	now := int64(1_000_000_000)
	p := DefaultParams()

	q := lotsAged(now, [2]int64{1_000_000, 30})

	const bound = uint32(4)
	for i := 0; i < 20; i++ {
		q = InsertLot(q, CoinLot{Amount: "1", AcquiredAt: now - int64(i)}, bound)
		require.LessOrEqual(t, uint32(len(q)), bound, "the queue must stay bounded under a dust flood")
	}

	drift := q[0].AcquiredAt - (now - 30*day)
	require.GreaterOrEqual(t, drift, int64(0), "a merge can never report coin as OLDER than it is")
	require.Less(t, drift, int64(60),
		"dust is weighted by amount, so it cannot drag a large aged balance younger")
	require.True(t, IsOld(q[0].AcquiredAt, now, p.CoinAgeThresholdSeconds))

	consumed, _ := SpendOldestFirst(q, math.NewInt(1_000_000), now, p.CoinAgeThresholdSeconds)
	penalty := PenaltyForLots(consumed, now, p)
	require.Equal(t, "2000", penalty.String(),
		"1,000,000 uphi of aged coin still redeems at 0.2% (2,000 uphi) after being dusted")

	require.Equal(t, "1000020", TotalLots(q).String(), "merging conserves the amount exactly")
}

// The merge is amount-preserving and order-preserving under any sequence of insertions — the property the whole bound rests on.
func TestBoundLots_ConservesAmountAndOrder(t *testing.T) {
	now := int64(1_000_000_000)
	for _, bound := range []uint32{2, 3, 8, 64} {
		t.Run(fmt.Sprintf("bound_%d", bound), func(t *testing.T) {
			var q []CoinLot
			total := math.ZeroInt()
			for i := 0; i < 100; i++ {
				amt := math.NewInt(int64(i + 1))
				q = InsertLot(q, CoinLot{Amount: amt.String(), AcquiredAt: now - int64(100-i)*day}, bound)
				total = total.Add(amt)

				require.LessOrEqual(t, uint32(len(q)), bound)
				for j := 1; j < len(q); j++ {
					require.LessOrEqual(t, q[j-1].AcquiredAt, q[j].AcquiredAt, "queue must stay oldest-first")
				}
			}
			require.Equal(t, total.String(), TotalLots(q).String(), "no uphi is created or lost by merging")
		})
	}
}
