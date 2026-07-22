// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"encoding/binary"
	"sort"

	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/governance/types"
)

// This file holds the running one-human-one-vote tally: the per-option counts, the turnout, and the per-voter record of what each voter last contributed.

// RunningTally is the accumulated result of a proposal's public-route votes.
type RunningTally struct {
	// Counts is the number of counted ballots per vote option, indexed by the option's numeric value.
	Counts map[int32]uint64
	// Turnout is the number of distinct eligible controllers that have voted.
	Turnout uint64
}

// FrozenEligibility is the per-proposal eligibility basis, taken once when the proposal enters its voting period and never recomputed.
type FrozenEligibility struct {
	Denominator uint64
	Cutoff      int64
	// FrozenAt is the block time at which this basis was taken.
	FrozenAt int64
}

// SetProposalEligibility stores the frozen eligibility basis for a proposal.
func (k Keeper) SetProposalEligibility(ctx sdk.Context, proposalID uint64, e FrozenEligibility) {
	b := make([]byte, 24)
	binary.BigEndian.PutUint64(b[:8], e.Denominator)
	binary.BigEndian.PutUint64(b[8:16], uint64(e.Cutoff))
	binary.BigEndian.PutUint64(b[16:], uint64(e.FrozenAt))
	ctx.KVStore(k.storeKey).Set(types.ProposalEligibilityKey(proposalID), b)
}

// GetProposalEligibility returns the frozen eligibility basis, if one has been taken.
func (k Keeper) GetProposalEligibility(ctx sdk.Context, proposalID uint64) (FrozenEligibility, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.ProposalEligibilityKey(proposalID))
	e := FrozenEligibility{}
	switch len(bz) {
	case 24:
		e.FrozenAt = int64(binary.BigEndian.Uint64(bz[16:]))
		fallthrough
	case 16: // written before FrozenAt existed
		e.Denominator = binary.BigEndian.Uint64(bz[:8])
		e.Cutoff = int64(binary.BigEndian.Uint64(bz[8:16]))
	default:
		return FrozenEligibility{}, false
	}
	return e, true
}

// GetRunningTally returns the accumulated counts and turnout for a proposal.
func (k Keeper) GetRunningTally(ctx sdk.Context, proposalID uint64) RunningTally {
	store := ctx.KVStore(k.storeKey)
	t := RunningTally{Counts: map[int32]uint64{}}

	prefix := append(append([]byte{}, types.TallyCountPrefix...), types.Uint64Key(proposalID)...)
	it := storetypes.KVStorePrefixIterator(store, prefix)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		key := it.Key()
		if len(key) != len(prefix)+1 {
			continue
		}
		t.Counts[int32(key[len(prefix)])] = decodeUint64(it.Value())
	}
	t.Turnout = decodeUint64(store.Get(types.TallyTurnoutKey(proposalID)))
	return t
}

func (k Keeper) addOptionCount(ctx sdk.Context, proposalID uint64, option int32, delta int64) {
	store := ctx.KVStore(k.storeKey)
	key := types.TallyCountKey(proposalID, option)
	cur := decodeUint64(store.Get(key))
	store.Set(key, encodeUint64(applyDelta(cur, delta)))
}

func (k Keeper) addTurnout(ctx sdk.Context, proposalID uint64, delta int64) {
	store := ctx.KVStore(k.storeKey)
	cur := decodeUint64(store.Get(types.TallyTurnoutKey(proposalID)))
	store.Set(types.TallyTurnoutKey(proposalID), encodeUint64(applyDelta(cur, delta)))
}

// GetCountedVote returns what this voter was last counted under: the recorded option, or the ineligible marker.
func (k Keeper) GetCountedVote(ctx sdk.Context, proposalID uint64, voter []byte) (byte, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.CountedVoteKey(proposalID, voter))
	if len(bz) != 1 {
		return 0, false
	}
	return bz[0], true
}

func (k Keeper) setCountedVote(ctx sdk.Context, proposalID uint64, voter []byte, marker byte) {
	ctx.KVStore(k.storeKey).Set(types.CountedVoteKey(proposalID, voter), []byte{marker})
}

// RecordVote folds one cast ballot into the running tally and returns whether it was counted.
func (k Keeper) RecordVote(ctx sdk.Context, proposalID uint64, voter []byte, option int32, eligible bool) bool {
	prev, had := k.GetCountedVote(ctx, proposalID, voter)

	if had && prev == types.IneligibleVoteMarker {
		return false // already judged ineligible for this proposal; nothing to re-evaluate
	}
	if !had && !eligible {
		k.setCountedVote(ctx, proposalID, voter, types.IneligibleVoteMarker)
		return false
	}

	if had {
		// A changed ballot: retract the previous contribution before recording the new one.
		if int32(prev) == option {
			return true // unchanged; the counts already reflect this voter
		}
		k.addOptionCount(ctx, proposalID, int32(prev), -1)
	} else {
		k.addTurnout(ctx, proposalID, +1)
	}

	k.addOptionCount(ctx, proposalID, option, +1)
	k.setCountedVote(ctx, proposalID, voter, byte(option))
	return true
}

// TalliedProposalIDs returns, in ascending id order, every proposal that still holds public-route tally state: a frozen eligibility basis, a turnout counter, or at least one per-voter contribution record.
func (k Keeper) TalliedProposalIDs(ctx sdk.Context) []uint64 {
	store := ctx.KVStore(k.storeKey)
	seen := map[uint64]struct{}{}

	for _, prefix := range [][]byte{
		types.ProposalEligibilityPrefix,
		types.TallyTurnoutPrefix,
		types.CountedVotePrefix,
		types.TallyCountPrefix,
	} {
		it := storetypes.KVStorePrefixIterator(store, prefix)
		for ; it.Valid(); it.Next() {
			key := it.Key()
			if len(key) < len(prefix)+8 {
				continue
			}
			seen[binary.BigEndian.Uint64(key[len(prefix):len(prefix)+8])] = struct{}{}
		}
		_ = it.Close()
	}

	ids := make([]uint64, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// IterateCountedVotes visits every per-voter contribution record of one proposal, passing the voter address and the recorded marker (a vote option, or types.IneligibleVoteMarker).
func (k Keeper) IterateCountedVotes(ctx sdk.Context, proposalID uint64, fn func(voter []byte, marker byte) bool) {
	prefix := types.CountedVotePrefixFor(proposalID)
	it := storetypes.KVStorePrefixIterator(ctx.KVStore(k.storeKey), prefix)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		value := it.Value()
		if len(value) != 1 {
			continue
		}
		if fn(append([]byte(nil), it.Key()[len(prefix):]...), value[0]) {
			return
		}
	}
}

// EnqueueForPruning marks a proposal's vote records for deletion.
func (k Keeper) EnqueueForPruning(ctx sdk.Context, proposalID uint64) {
	ctx.KVStore(k.storeKey).Set(types.PruneKey(proposalID), []byte{1})
}

// IsQueuedForPruning reports whether a proposal is still awaiting pruning.
func (k Keeper) IsQueuedForPruning(ctx sdk.Context, proposalID uint64) bool {
	return ctx.KVStore(k.storeKey).Has(types.PruneKey(proposalID))
}

// PruneStep deletes at most budget records belonging to queued proposals and returns how many it deleted.
func (k Keeper) PruneStep(ctx sdk.Context, budget int) int {
	if budget <= 0 {
		return 0
	}
	store := ctx.KVStore(k.storeKey)
	deleted := 0

	qit := storetypes.KVStorePrefixIterator(store, types.PrunePrefix)
	var queued []uint64
	for ; qit.Valid(); qit.Next() {
		key := qit.Key()
		if len(key) != len(types.PrunePrefix)+8 {
			continue
		}
		queued = append(queued, binary.BigEndian.Uint64(key[len(types.PrunePrefix):]))
	}
	_ = qit.Close()

	for _, proposalID := range queued {
		if deleted >= budget {
			return deleted
		}

		var batch [][]byte
		vit := storetypes.KVStorePrefixIterator(store, types.CountedVotePrefixFor(proposalID))
		for ; vit.Valid() && len(batch) < budget-deleted; vit.Next() {
			batch = append(batch, append([]byte(nil), vit.Key()...))
		}
		_ = vit.Close()
		for _, key := range batch {
			store.Delete(key)
			deleted++
		}
		if len(batch) > 0 {
			continue // more may remain for this proposal; resume next block
		}

		// This proposal's per-voter records are gone: drop its aggregates and leave the queue.
		tit := storetypes.KVStorePrefixIterator(store,
			append(append([]byte{}, types.TallyCountPrefix...), types.Uint64Key(proposalID)...))
		var tallyKeys [][]byte
		for ; tit.Valid(); tit.Next() {
			tallyKeys = append(tallyKeys, append([]byte(nil), tit.Key()...))
		}
		_ = tit.Close()
		for _, key := range tallyKeys {
			store.Delete(key)
			deleted++
		}
		store.Delete(types.TallyTurnoutKey(proposalID))
		store.Delete(types.ProposalEligibilityKey(proposalID))
		store.Delete(types.PruneKey(proposalID))
		deleted++
	}
	return deleted
}

func encodeUint64(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

func decodeUint64(bz []byte) uint64 {
	if len(bz) != 8 {
		return 0
	}
	return binary.BigEndian.Uint64(bz)
}

func applyDelta(cur uint64, delta int64) uint64 {
	if delta < 0 {
		d := uint64(-delta)
		if d > cur {
			return 0
		}
		return cur - d
	}
	return cur + uint64(delta)
}
