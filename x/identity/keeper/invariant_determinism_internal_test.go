// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"fmt"
	"testing"
	"time"

	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

func seedBrokenEligibility(t *testing.T, k Keeper, ctx sdk.Context, n, broken int) {
	t.Helper()
	for i := 0; i < n; i++ {
		controller := sdk.AccAddress([]byte(fmt.Sprintf("determinism-ctrl-%03d", i))).String()
		k.SetIdentity(ctx, types.DIDDocument{
			Did: fmt.Sprintf("did:phi:determinism-%03d", i), Controller: controller,
			Status: types.DID_STATUS_ACTIVE, CreatedAt: ctx.BlockTime().Unix() - int64(i),
			PubKey: []byte("pk"), UniquenessHash: []byte(fmt.Sprintf("uniq-%03d", i)),
		})
	}
	store := ctx.KVStore(k.storeKey)
	it := storetypes.KVStorePrefixIterator(store, types.ControllerEligibilityPrefix)
	var keys [][]byte
	for ; it.Valid(); it.Next() {
		keys = append(keys, append([]byte(nil), it.Key()...))
	}
	require.NoError(t, it.Close())
	require.Len(t, keys, n, "every controller must have had a record before any is deleted")
	require.LessOrEqual(t, broken, n)
	for _, key := range keys[n-broken:] {
		store.Delete(key)
	}
}

// TestEligibilityInvariant_IsDeterministicUnderRepeatedRuns is the property: the same state produces the same gas consumption and the same message, every time.
func TestEligibilityInvariant_IsDeterministicUnderRepeatedRuns(t *testing.T) {
	const controllers = 64
	const runs = 50

	ctx, k := setupInternal(t)
	ctx = ctx.WithBlockTime(time.Unix(1_700_000_000, 0))
	seedBrokenEligibility(t, k, ctx, controllers, 1)

	inv := EligibilityIndexInvariant(k)

	gasSeen := map[uint64]int{}
	msgSeen := map[string]int{}
	for i := 0; i < runs; i++ {
		runCtx := ctx.WithGasMeter(storetypes.NewGasMeter(50_000_000))
		msg, broken := inv(runCtx)
		require.True(t, broken, "the seeded state is broken; the invariant must say so")
		gasSeen[runCtx.GasMeter().GasConsumed()]++
		msgSeen[msg]++
	}

	require.Len(t, gasSeen, 1,
		"the invariant consumed %d different amounts of gas on identical state: %v — gas_used is hashed "+
			"into the block results, so two honest validators running this would fork", len(gasSeen), gasSeen)
	require.Len(t, msgSeen, 1,
		"the invariant reported %d different breaks on identical state — two nodes halting on the same "+
			"corruption would halt with different reasons", len(msgSeen))
}

// With MANY simultaneous breaks the loop finds a random one of them, so the REPORTED break is what diverges.
func TestEligibilityInvariant_ReportsTheSameBreakEveryTime(t *testing.T) {
	const controllers = 64
	const runs = 50

	ctx, k := setupInternal(t)
	ctx = ctx.WithBlockTime(time.Unix(1_700_000_000, 0))
	seedBrokenEligibility(t, k, ctx, controllers, controllers/2)

	inv := EligibilityIndexInvariant(k)
	msgSeen := map[string]int{}
	for i := 0; i < runs; i++ {
		msg, broken := inv(ctx)
		require.True(t, broken)
		msgSeen[msg]++
	}
	require.Len(t, msgSeen, 1,
		"the invariant reported %d different breaks on identical state", len(msgSeen))
}

// The same property across DIFFERENT insertion orders.
func TestEligibilityInvariant_IsDeterministicAcrossInsertionOrders(t *testing.T) {
	const controllers = 32

	measure := func(reverse bool) (uint64, string) {
		ctx, k := setupInternal(t)
		ctx = ctx.WithBlockTime(time.Unix(1_700_000_000, 0))

		order := make([]int, controllers)
		for i := range order {
			if reverse {
				order[i] = controllers - 1 - i
			} else {
				order[i] = i
			}
		}
		for _, i := range order {
			controller := sdk.AccAddress([]byte(fmt.Sprintf("determinism-ctrl-%03d", i))).String()
			k.SetIdentity(ctx, types.DIDDocument{
				Did: fmt.Sprintf("did:phi:determinism-%03d", i), Controller: controller,
				Status: types.DID_STATUS_ACTIVE, CreatedAt: ctx.BlockTime().Unix() - int64(i),
				PubKey: []byte("pk"), UniquenessHash: []byte(fmt.Sprintf("uniq-%03d", i)),
			})
		}
		store := ctx.KVStore(k.storeKey)
		it := storetypes.KVStorePrefixIterator(store, types.ControllerEligibilityPrefix)
		var keys [][]byte
		for ; it.Valid(); it.Next() {
			keys = append(keys, append([]byte(nil), it.Key()...))
		}
		require.NoError(t, it.Close())
		for _, key := range keys {
			store.Delete(key)
		}

		runCtx := ctx.WithGasMeter(storetypes.NewGasMeter(50_000_000))
		msg, broken := EligibilityIndexInvariant(k)(runCtx)
		require.True(t, broken)
		return runCtx.GasMeter().GasConsumed(), msg
	}

	forwardGas, forwardMsg := measure(false)
	reverseGas, reverseMsg := measure(true)

	require.Equal(t, forwardGas, reverseGas,
		"gas consumption must not depend on the order the registry was built in")
	require.Equal(t, forwardMsg, reverseMsg,
		"the reported break must not depend on the order the registry was built in")
}

// Healthy state must stay healthy, and cost the same every time — the ordinary case, which is the one that actually runs in every block that carries a MsgVerifyInvariant.
func TestEligibilityInvariant_HealthyStateIsAlsoDeterministic(t *testing.T) {
	ctx, k := setupInternal(t)
	ctx = ctx.WithBlockTime(time.Unix(1_700_000_000, 0))
	for i := 0; i < 32; i++ {
		k.SetIdentity(ctx, types.DIDDocument{
			Did:        fmt.Sprintf("did:phi:healthy-%03d", i),
			Controller: sdk.AccAddress([]byte(fmt.Sprintf("healthy-ctrl-%07d", i))).String(),
			Status:     types.DID_STATUS_ACTIVE, CreatedAt: ctx.BlockTime().Unix() - int64(i),
			PubKey: []byte("pk"), UniquenessHash: []byte(fmt.Sprintf("uniq-healthy-%03d", i)),
		})
	}

	gasSeen := map[uint64]int{}
	for i := 0; i < 20; i++ {
		runCtx := ctx.WithGasMeter(storetypes.NewGasMeter(50_000_000))
		msg, broken := EligibilityIndexInvariant(k)(runCtx)
		require.False(t, broken, msg)
		gasSeen[runCtx.GasMeter().GasConsumed()]++
	}
	require.Len(t, gasSeen, 1, "a healthy run must cost the same every time: %v", gasSeen)
}
