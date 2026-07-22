// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

const testCeiling = 2_000

func seedAgeMirror(ctx sdk.Context, key storetypes.StoreKey, n int, createdAt int64) {
	store := ctx.KVStore(key)
	for i := 0; i < n; i++ {
		store.Set(types.EligibilityByAgeKey(createdAt, fmt.Sprintf("phi1controller%09d", i)), []byte{1})
	}
}

func setEligibleTotal(ctx sdk.Context, key storetypes.StoreKey, n uint64) {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, n)
	ctx.KVStore(key).Set(types.EligibleControllerTotalKey, b)
}

func scanUnderInfiniteGas(ctx sdk.Context, k Keeper, cutoff int64) (tail uint64, truncated bool, gas uint64) {
	meter := storetypes.NewInfiniteGasMeter()
	n, cut := k.countEligibleNewerThanCapped(ctx.WithGasMeter(meter), cutoff, testCeiling)
	return n, cut, meter.GasConsumed()
}

func tailFixture(t *testing.T, size int, total uint64) (sdk.Context, Keeper, int64) {
	t.Helper()
	ctx, k := setupInternal(t)
	cutoff := int64(2_000_000)
	seedAgeMirror(ctx, k.storeKey, size, cutoff+1_000)
	setEligibleTotal(ctx, k.storeKey, total)
	return ctx, k, cutoff
}

// TestEligibilityTail_WorkDoesNotGrowWithTheRegistry is the bound, stated as the property that matters: two registries of different sizes, BOTH larger than the ceiling and both entirely tail, must cost the SAME to scan.
func TestEligibilityTail_WorkDoesNotGrowWithTheRegistry(t *testing.T) {
	measure := func(size int) uint64 {
		ctx, k, cutoff := tailFixture(t, size, uint64(size))
		_, _, gas := scanUnderInfiniteGas(ctx, k, cutoff)
		return gas
	}

	smaller := measure(testCeiling + 500)
	larger := measure(testCeiling + 5_000)

	require.Equal(t, smaller, larger,
		"a larger registry must cost exactly the same: the ceiling, not the registry, ends the walk")
}

// The measurement has to be real, or the equality above would be vacuous.
func TestEligibilityTail_CostTracksTheTailBelowTheCeiling(t *testing.T) {
	measure := func(size int) uint64 {
		ctx, k, cutoff := tailFixture(t, size, uint64(size))
		_, _, gas := scanUnderInfiniteGas(ctx, k, cutoff)
		return gas
	}

	require.Less(t, measure(100), measure(1_000),
		"below the ceiling a longer tail genuinely costs more; the gas measurement is not a constant")
}

// TestEligibilityTail_EveryRegistrySizeStaysWithinTheCeiling walks the full range and asserts the scan never examines more than the ceiling, whatever the registry holds.
func TestEligibilityTail_EveryRegistrySizeStaysWithinTheCeiling(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
	}{
		{"empty registry", 0},
		{"one controller", 1},
		{"a few hundred, all tail", 500},
		{"at the ceiling", testCeiling},
		{"past the ceiling", testCeiling + 5_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, k, cutoff := tailFixture(t, tc.size, uint64(tc.size))
			tail, truncated, _ := scanUnderInfiniteGas(ctx, k, cutoff)

			require.LessOrEqual(t, tail, uint64(testCeiling),
				"a registry of %d must be scanned within the ceiling", tc.size)
			require.Equal(t, tc.size > testCeiling, truncated,
				"truncation must be reported exactly when the ceiling is reached")
		})
	}
}

// Truncation is FAIL-SAFE, which is what makes a hard ceiling acceptable at all.
func TestEligibilityTail_TruncationErrsTowardAHarderQuorum(t *testing.T) {
	size := testCeiling + 5_000
	total := uint64(size + 10_000)
	ctx, k, cutoff := tailFixture(t, size, total)

	tail, truncated, _ := scanUnderInfiniteGas(ctx, k, cutoff)
	require.True(t, truncated)
	require.Equal(t, uint64(testCeiling), tail, "the scan counts exactly the ceiling")

	trueDenominator := total - uint64(size)
	truncatedDenominator := total - tail
	require.Greater(t, truncatedDenominator, trueDenominator,
		"a truncated tail must produce a LARGER denominator than the true one")
}

// The degenerate ends of the denominator.
func TestEligibilityDenominator_DegenerateTailsAreHandledByTheirCause(t *testing.T) {
	for _, tc := range []struct {
		name  string
		size  int
		total uint64
		want  uint64
	}{
		{"whole registry newer than the cutoff", 500, 500, 0},
		{"mirror holds more entries than the total", 500, 300, 300},
		{"a genuinely empty registry", 0, 0, 0},
		{"an ordinary partial tail", 100, 400, 300},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, k := setupInternal(t)
			cutoff := int64(2_000_000)
			seedAgeMirror(ctx, k.storeKey, tc.size, cutoff+1_000)
			setEligibleTotal(ctx, k.storeKey, tc.total)

			got := k.CountEligibleControllersAt(ctx, time.Unix(cutoff, 0), 0)
			require.Equal(t, tc.want, got)
			require.LessOrEqual(t, got, tc.total,
				"the denominator can never exceed the eligible-controller total")
		})
	}
}

// The production ceiling is what the production scan uses, and it is far above any tail a test seeds.
func TestEligibilityTail_ProductionCeilingIsTheOneInForce(t *testing.T) {
	require.Greater(t, uint64(MaxEligibilityTailScan), uint64(testCeiling),
		"the production ceiling must exceed what these tests exercise, or they prove nothing about it")

	ctx, k, cutoff := tailFixture(t, testCeiling+5_000, uint64(testCeiling+5_000))
	tail, truncated := k.countEligibleNewerThan(ctx, cutoff)
	require.False(t, truncated, "the production ceiling is nowhere near this registry")
	require.Equal(t, uint64(testCeiling+5_000), tail, "and the tail is therefore counted exactly")
}
