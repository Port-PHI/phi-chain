// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

// Bounded, deferred institution removal: RemoveInstitution deletes single authority records synchronously and enqueues the id; SweepRemovals drains the ranged per-institution keyspaces under a per-block budget.

func (k Keeper) enqueueRemoval(ctx sdk.Context, id string) {
	ctx.KVStore(k.storeKey).Set(types.RemovalQueueKey(id), []byte{types.RemovalQueueMarkerByte})
}

func (k Keeper) dequeueRemoval(ctx sdk.Context, id string) {
	ctx.KVStore(k.storeKey).Delete(types.RemovalQueueKey(id))
}

// HasPendingRemoval reports whether an institution id is mid-removal; re-registration is blocked while it holds.
func (k Keeper) HasPendingRemoval(ctx sdk.Context, id string) bool {
	return ctx.KVStore(k.storeKey).Has(types.RemovalQueueKey(id))
}

func (k Keeper) firstPendingRemoval(ctx sdk.Context) (string, bool) {
	it := storetypes.KVStorePrefixIterator(ctx.KVStore(k.storeKey), types.RemovalQueuePrefix)
	defer it.Close()
	if !it.Valid() {
		return "", false
	}
	return types.ParseRemovalQueueKey(it.Key())
}

// SweepRemovals drains enqueued institution removals under a fixed per-block budget (called from BeginBlock).
func (k Keeper) SweepRemovals(ctx sdk.Context) {
	budget := types.RemovalPruneBudget
	for budget > 0 {
		id, ok := k.firstPendingRemoval(ctx)
		if !ok {
			return
		}
		budget -= k.drainInstitutionRanged(ctx, id, budget)
		if k.institutionRangedEmpty(ctx, id) {
			// Fully reclaimed: retire the queue entry (also draws from the budget) and move to the next id.
			k.dequeueRemoval(ctx, id)
			budget--
			continue
		}
		return // budget exhausted mid-institution; the sweep resumes here next block
	}
}

func (k Keeper) drainInstitutionRanged(ctx sdk.Context, id string, budget int) int {
	store := ctx.KVStore(k.storeKey)
	deleted := 0
	for _, prefix := range types.PerInstitutionRangePrefixes(id) {
		if deleted >= budget {
			break
		}
		var keys [][]byte
		it := storetypes.KVStorePrefixIterator(store, prefix)
		for ; it.Valid() && deleted+len(keys) < budget; it.Next() {
			keys = append(keys, append([]byte(nil), it.Key()...))
		}
		_ = it.Close()
		for _, key := range keys {
			store.Delete(key)
		}
		deleted += len(keys)
	}
	return deleted
}

func (k Keeper) institutionRangedEmpty(ctx sdk.Context, id string) bool {
	store := ctx.KVStore(k.storeKey)
	for _, prefix := range types.PerInstitutionRangePrefixes(id) {
		it := storetypes.KVStorePrefixIterator(store, prefix)
		valid := it.Valid()
		_ = it.Close()
		if valid {
			return false
		}
	}
	return true
}
