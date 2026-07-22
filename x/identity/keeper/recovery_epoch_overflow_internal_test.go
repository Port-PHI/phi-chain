// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

// TestBumpGuardianEpoch_DoesNotWrapToZero drives the epoch to the saturated value and proves a further bump holds it there rather than wrapping to the epoch-0 sentinel.
func TestBumpGuardianEpoch_DoesNotWrapToZero(t *testing.T) {
	ctx, k := setupInternal(t)
	const did = "did:phi:epoch-overflow"

	ctx.KVStore(k.storeKey).Set(types.GuardianEpochKey(did), encodeEpoch(math.MaxUint64))
	require.Equal(t, uint64(math.MaxUint64), k.GuardianEpoch(ctx, did))

	k.bumpGuardianEpoch(ctx, did)
	require.Equal(t, uint64(math.MaxUint64), k.GuardianEpoch(ctx, did),
		"a saturated epoch must hold at the ceiling, never wrap to the epoch-0 sentinel")
	require.NotZero(t, k.GuardianEpoch(ctx, did), "wrapping to 0 would revive every retired tally")

	ctx.KVStore(k.storeKey).Set(types.GuardianEpochKey(did), encodeEpoch(math.MaxUint64-1))
	k.bumpGuardianEpoch(ctx, did)
	require.Equal(t, uint64(math.MaxUint64), k.GuardianEpoch(ctx, did))
	k.bumpGuardianEpoch(ctx, did)
	require.Equal(t, uint64(math.MaxUint64), k.GuardianEpoch(ctx, did), "and then holds")
}

// A pinned-at-ceiling epoch still retires a tally stamped under an earlier epoch: equality, not advancement, is what tallyIsCurrent tests, so the guard does not weaken rotation.
func TestBumpGuardianEpoch_SaturatedEpochStillRetiresOldTallies(t *testing.T) {
	ctx, k := setupInternal(t)
	const did = "did:phi:epoch-retire"

	ctx.KVStore(k.storeKey).Set(types.GuardianEpochKey(did), encodeEpoch(math.MaxUint64))
	r := types.RecoveryRequest{RecoveryId: []byte("rid"), Did: did}
	k.setRecoveryTallyEpoch(ctx, r.RecoveryId, math.MaxUint64-1) // stamped under an earlier set

	require.False(t, k.tallyIsCurrent(ctx, r),
		"a tally stamped under an earlier epoch is not current even when the live epoch is pinned")
}
