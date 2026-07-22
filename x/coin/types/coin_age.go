// SPDX-License-Identifier: Apache-2.0

package types

import "cosmossdk.io/math"

// This file is the FIFO coin-age core: pure functions over a holder's lot queue, with no state and no block context, so the mechanics that decide the early-redeem penalty can be reasoned about and tested on their own.

// LotAmount returns a lot's amount (a malformed amount reads as zero, never as a panic).
func LotAmount(l CoinLot) math.Int { return parseAmount(l.Amount) }

// TotalLots returns the total uphi tracked in a queue.
func TotalLots(lots []CoinLot) math.Int {
	total := math.ZeroInt()
	for _, l := range lots {
		total = total.Add(LotAmount(l))
	}
	return total
}

// IsOld reports whether a lot acquired at `acquiredAt` has reached the old-coin tier at `now`.
func IsOld(acquiredAt, now, thresholdSeconds int64) bool {
	return now-acquiredAt >= thresholdSeconds
}

// PenaltyForLots sums the early-redeem penalty over consumed lots, pricing EACH lot at its OWN age tier.
func PenaltyForLots(lots []CoinLot, now int64, p Params) math.Int {
	penalty := math.ZeroInt()
	for _, l := range lots {
		bps := int64(p.YoungPenaltyBps)
		if IsOld(l.AcquiredAt, now, p.CoinAgeThresholdSeconds) {
			bps = int64(p.OldPenaltyBps)
		}
		penalty = penalty.Add(LotAmount(l).MulRaw(bps).QuoRaw(BpsDenominator))
	}
	return penalty
}

// SpendOldestFirst consumes `amount` from the front (oldest end) of the queue and returns the lots actually consumed — each carrying its own acquired_at — plus the remaining queue.
func SpendOldestFirst(lots []CoinLot, amount math.Int, now, thresholdSeconds int64) (consumed, remaining []CoinLot) {
	left := amount
	i := 0
	for ; i < len(lots) && left.IsPositive(); i++ {
		lotAmt := LotAmount(lots[i])
		if lotAmt.LTE(left) {
			// The whole lot goes.
			consumed = append(consumed, CoinLot{Amount: lotAmt.String(), AcquiredAt: lots[i].AcquiredAt})
			left = left.Sub(lotAmt)
			continue
		}
		// Partial: split the lot, keeping its age on both halves.
		consumed = append(consumed, CoinLot{Amount: left.String(), AcquiredAt: lots[i].AcquiredAt})
		remaining = append(remaining, CoinLot{Amount: lotAmt.Sub(left).String(), AcquiredAt: lots[i].AcquiredAt})
		left = math.ZeroInt()
		i++
		break
	}
	remaining = append(remaining, lots[i:]...)

	if left.IsPositive() {
		// Untracked surplus: priced as old (see above).
		consumed = append(consumed, CoinLot{
			Amount:     left.String(),
			AcquiredAt: now - thresholdSeconds, // exactly at the old-tier boundary
		})
	}
	return consumed, remaining
}

// InsertLot places a lot into the queue in oldest-first order and enforces the length bound.
func InsertLot(lots []CoinLot, lot CoinLot, maxLots uint32) []CoinLot {
	if !LotAmount(lot).IsPositive() {
		return lots
	}
	pos := len(lots)
	for i := range lots {
		if lot.AcquiredAt < lots[i].AcquiredAt {
			pos = i
			break
		}
	}
	out := make([]CoinLot, 0, len(lots)+1)
	out = append(out, lots[:pos]...)
	out = append(out, lot)
	out = append(out, lots[pos:]...)
	return BoundLots(out, maxLots)
}

// BoundLots enforces the queue's length bound by merging at the OLD end.
func BoundLots(lots []CoinLot, maxLots uint32) []CoinLot {
	if maxLots == 0 {
		maxLots = DefaultMaxCoinAgeLots
	}
	for uint32(len(lots)) > maxLots && len(lots) >= 2 {
		merged := CoinLot{
			Amount:     LotAmount(lots[0]).Add(LotAmount(lots[1])).String(),
			AcquiredAt: mergedAcquiredAt(lots[0], lots[1]),
		}
		lots = append([]CoinLot{merged}, lots[2:]...)
	}
	return lots
}

func mergedAcquiredAt(older, newer CoinLot) int64 {
	delta := newer.AcquiredAt - older.AcquiredAt
	if delta <= 0 {
		// Equal timestamps, or a queue that is not oldest-first: there is nothing to weight.
		return older.AcquiredAt
	}
	total := LotAmount(older).Add(LotAmount(newer))
	if !total.IsPositive() {
		return older.AcquiredAt
	}
	// ceil(a_newer × delta ÷ total), all operands non-negative.
	num := LotAmount(newer).MulRaw(delta)
	return older.AcquiredAt + num.Add(total).SubRaw(1).Quo(total).Int64()
}

func parseAmount(s string) math.Int {
	v, ok := math.NewIntFromString(s)
	if !ok || v.IsNegative() {
		return math.ZeroInt()
	}
	return v
}
