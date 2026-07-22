// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

// PruneStaleCounters deletes past-day transient cap counters under a per-block budget (BeginBlock); never resets a live cap, moves no value.
func (k Keeper) PruneStaleCounters(ctx sdk.Context) {
	staleBefore := dayIndex(ctx) - types.CounterRetentionDays
	if staleBefore <= 0 {
		return
	}
	k.pruneRedeemSubjectCounters(ctx, staleBefore)
	k.pruneCounterPrefix(ctx, staleBefore)
}

func (k Keeper) pruneRedeemSubjectCounters(ctx sdk.Context, staleBefore int64) {
	store := ctx.KVStore(k.storeKey)
	it := store.Iterator(types.RedeemSubjectPrefix, types.RedeemSubjectDayBound(staleBefore))
	defer it.Close()
	var stale [][]byte
	for n := 0; it.Valid() && n < types.CounterPruneBudget; it.Next() {
		stale = append(stale, append([]byte(nil), it.Key()...))
		n++
	}
	for _, key := range stale {
		store.Delete(key)
	}
}

func (k Keeper) pruneCounterPrefix(ctx sdk.Context, staleBefore int64) {
	store := ctx.KVStore(k.storeKey)
	cursorKey := types.CounterPruneCursorKey()

	start := append([]byte{}, types.CounterPrefix...)
	if cur := store.Get(cursorKey); len(cur) > 0 {
		start = append(append([]byte(nil), cur...), 0x00)
	}
	end := storetypes.PrefixEndBytes(types.CounterPrefix)

	it := store.Iterator(start, end)
	var (
		stale    [][]byte
		last     []byte
		examined int
	)
	for ; it.Valid() && examined < types.CounterPruneBudget; it.Next() {
		key := append([]byte(nil), it.Key()...)
		last = key
		examined++
		if kind, day, ok := types.ParseCounterKeyDay(key); ok && types.IsPrunableCounterKind(kind) && day < staleBefore {
			stale = append(stale, key)
		}
	}
	exhausted := !it.Valid()
	_ = it.Close()

	for _, key := range stale {
		store.Delete(key)
	}

	// Advance cursor; wrap to prefix start (delete cursor) when the ring completes.
	if exhausted || last == nil {
		store.Delete(cursorKey)
	} else {
		store.Set(cursorKey, last)
	}
}
