// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"encoding/binary"
	"math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

// Guardian-set epoch: each DID carries a counter bumped on set replacement, each request stamps the epoch its tally was collected under; a tally stamped with a superseded epoch counts for nothing.

// GuardianEpoch returns the DID's guardian-set epoch; zero means never replaced (or no set).
func (k Keeper) GuardianEpoch(ctx sdk.Context, did string) uint64 {
	return decodeEpoch(ctx.KVStore(k.storeKey).Get(types.GuardianEpochKey(did)))
}

func (k Keeper) bumpGuardianEpoch(ctx sdk.Context, did string) {
	cur := k.GuardianEpoch(ctx, did)
	if cur == math.MaxUint64 {
		return // hold at ceiling; wrapping to the epoch-0 sentinel would revalidate stale tallies
	}
	ctx.KVStore(k.storeKey).Set(types.GuardianEpochKey(did), encodeEpoch(cur+1))
}

func (k Keeper) recoveryTallyEpoch(ctx sdk.Context, recoveryID []byte) uint64 {
	return decodeEpoch(ctx.KVStore(k.storeKey).Get(types.RecoveryTallyEpochKey(recoveryID)))
}

func (k Keeper) setRecoveryTallyEpoch(ctx sdk.Context, recoveryID []byte, epoch uint64) {
	ctx.KVStore(k.storeKey).Set(types.RecoveryTallyEpochKey(recoveryID), encodeEpoch(epoch))
}

func (k Keeper) deleteRecoveryTallyEpoch(ctx sdk.Context, recoveryID []byte) {
	ctx.KVStore(k.storeKey).Delete(types.RecoveryTallyEpochKey(recoveryID))
}

func (k Keeper) tallyIsCurrent(ctx sdk.Context, r types.RecoveryRequest) bool {
	return k.recoveryTallyEpoch(ctx, r.RecoveryId) == k.GuardianEpoch(ctx, r.Did)
}

// EffectiveApprovals returns approvals collected under the current guardian set; superseded ones are ignored (not deleted here — the clear happens when a guardian next acts).
func (k Keeper) EffectiveApprovals(ctx sdk.Context, r types.RecoveryRequest) []string {
	if !k.tallyIsCurrent(ctx, r) {
		return nil
	}
	return r.Approvals
}

// EffectiveRejections is the rejection-side counterpart of EffectiveApprovals.
func (k Keeper) EffectiveRejections(ctx sdk.Context, r types.RecoveryRequest) []string {
	if !k.tallyIsCurrent(ctx, r) {
		return nil
	}
	return r.Rejections
}

func (k Keeper) syncTallyEpoch(ctx sdk.Context, r *types.RecoveryRequest) bool {
	current := k.GuardianEpoch(ctx, r.Did)
	if k.recoveryTallyEpoch(ctx, r.RecoveryId) == current {
		return false
	}
	cleared := len(r.Approvals) > 0 || len(r.Rejections) > 0
	r.Approvals = []string{}
	r.Rejections = []string{}
	k.setRecoveryTallyEpoch(ctx, r.RecoveryId, current)
	return cleared
}

func encodeEpoch(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

func decodeEpoch(bz []byte) uint64 {
	if len(bz) != 8 {
		return 0
	}
	return binary.BigEndian.Uint64(bz)
}
