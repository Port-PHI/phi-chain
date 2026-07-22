// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"

	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/keeper"
	"github.com/Port-PHI/phi-chain/x/identity/types"
)

func referenceDenominator(t *testing.T, k keeper.Keeper, ctx sdk.Context, cutoff int64) uint64 {
	t.Helper()
	oldest := map[string]int64{}
	k.IterateIdentities(ctx, func(d types.DIDDocument) bool {
		if d.Status != types.DID_STATUS_REVOKED {
			if cur, ok := oldest[d.Controller]; !ok || d.CreatedAt < cur {
				oldest[d.Controller] = d.CreatedAt
			}
		}
		return false
	})
	var n uint64
	for _, v := range oldest {
		if v <= cutoff {
			n++
		}
	}
	return n
}

func countAt(k keeper.Keeper, ctx sdk.Context, cutoff int64) uint64 {
	return k.CountEligibleControllersAt(ctx, time.Unix(cutoff, 0), 0)
}

func setDoc(k keeper.Keeper, ctx sdk.Context, did, controller string, status types.DIDStatus, createdAt int64) {
	k.SetIdentity(ctx, types.DIDDocument{
		Did: did, Controller: controller, Status: status, CreatedAt: createdAt,
		PubKey: []byte("pk"), UniquenessHash: []byte("uniq-" + did),
	})
}

// The denominator counts CONTROLLERS, not DIDs: several ACTIVE DIDs under one controller are one eligible human, and the controller becomes eligible at its OLDEST ACTIVE DID's age.
func TestEligibility_CountsControllersAtOldestActiveDID(t *testing.T) {
	ctx, k, _ := setupIdentity(t)

	setDoc(k, ctx, "did:phi:a1", "ctrlA", types.DID_STATUS_ACTIVE, 100)
	setDoc(k, ctx, "did:phi:a2", "ctrlA", types.DID_STATUS_ACTIVE, 50) // older; becomes the record
	setDoc(k, ctx, "did:phi:b1", "ctrlB", types.DID_STATUS_ACTIVE, 200)

	require.Equal(t, uint64(2), k.EligibleControllerTotal(ctx), "two controllers, not three DIDs")

	require.Equal(t, uint64(0), countAt(k, ctx, 49))
	require.Equal(t, uint64(1), countAt(k, ctx, 50), "ctrlA is eligible from its OLDEST DID")
	require.Equal(t, uint64(1), countAt(k, ctx, 199))
	require.Equal(t, uint64(2), countAt(k, ctx, 200))
}

// Revoking the oldest ACTIVE DID moves the controller's eligibility to the next-oldest; revoking the last one drops the controller out entirely.
func TestEligibility_RevocationMovesOrDropsTheRecord(t *testing.T) {
	ctx, k, _ := setupIdentity(t)

	setDoc(k, ctx, "did:phi:a1", "ctrlA", types.DID_STATUS_ACTIVE, 50)
	setDoc(k, ctx, "did:phi:a2", "ctrlA", types.DID_STATUS_ACTIVE, 100)
	require.Equal(t, uint64(1), countAt(k, ctx, 50))

	setDoc(k, ctx, "did:phi:a1", "ctrlA", types.DID_STATUS_REVOKED, 50)
	require.Equal(t, uint64(0), countAt(k, ctx, 50), "the oldest is gone; ctrlA is no longer that old")
	require.Equal(t, uint64(1), countAt(k, ctx, 100), "ctrlA is now eligible from its remaining DID")
	require.Equal(t, uint64(1), k.EligibleControllerTotal(ctx))

	setDoc(k, ctx, "did:phi:a2", "ctrlA", types.DID_STATUS_REVOKED, 100)
	require.Equal(t, uint64(0), countAt(k, ctx, 100))
	require.Equal(t, uint64(0), k.EligibleControllerTotal(ctx), "no ACTIVE DID left, no record")
}

// Suspension does NOT move a controller's standing: a SUSPENDED DID stays in the eligibility basis (non-revoked), so the controller remains counted in the denominator and its record is unchanged.
func TestEligibility_SuspensionKeepsTheStanding(t *testing.T) {
	ctx, k, _ := setupIdentity(t)
	ctx = ctx.WithBlockTime(time.Unix(1_000, 0))

	setDoc(k, ctx, "did:phi:a1", "ctrlA", types.DID_STATUS_ACTIVE, 50)
	require.Equal(t, uint64(1), countAt(k, ctx, 50))

	frozenAt := time.Unix(1_000, 0)
	require.True(t, k.IsEligibleControllerSince(ctx, "ctrlA", time.Unix(50, 0), 0, frozenAt))

	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	setDoc(k, ctx, "did:phi:a1", "ctrlA", types.DID_STATUS_SUSPENDED, 50)
	require.Equal(t, uint64(1), countAt(k, ctx, 50), "a suspended controller stays in the denominator")
	require.True(t, k.IsEligibleControllerAt(ctx, "ctrlA", time.Unix(50, 0), 0))

	ctx = ctx.WithBlockTime(time.Unix(3_000, 0))
	setDoc(k, ctx, "did:phi:a1", "ctrlA", types.DID_STATUS_ACTIVE, 50)
	require.Equal(t, uint64(1), countAt(k, ctx, 50))
	require.True(t, k.IsEligibleControllerSince(ctx, "ctrlA", time.Unix(50, 0), 0, frozenAt),
		"a controller counted at the freeze must still vote after a suspend/reinstate round trip")

	setDoc(k, ctx, "did:phi:a1", "ctrlA", types.DID_STATUS_REVOKED, 50)
	require.Equal(t, uint64(0), countAt(k, ctx, 50), "revocation drops the controller from the basis")
	require.False(t, k.IsEligibleControllerAt(ctx, "ctrlA", time.Unix(50, 0), 0))
}

// A key rotation rewrites the document without touching controller, status or created_at, so it must move nothing.
func TestEligibility_KeyRotationIsANoOp(t *testing.T) {
	ctx, k, _ := setupIdentity(t)

	setDoc(k, ctx, "did:phi:a1", "ctrlA", types.DID_STATUS_ACTIVE, 50)
	before := k.EligibleControllerTotal(ctx)

	for i := 0; i < 5; i++ {
		d, found := k.GetIdentity(ctx, "did:phi:a1")
		require.True(t, found)
		d.PubKey = []byte(fmt.Sprintf("rotated-%d", i))
		k.SetIdentity(ctx, d)
	}

	require.Equal(t, before, k.EligibleControllerTotal(ctx), "rotation must not change the total")
	require.Equal(t, uint64(1), countAt(k, ctx, 50))
	requireInvariantHolds(t, k, ctx)
}

// Social recovery is the only two-sided transition: it moves a DID from one controller to another, so the old controller may lose its last ACTIVE DID at the same moment the new one gains its first.
func TestEligibility_RecoveryUpdatesBothControllers(t *testing.T) {
	ctx, k, _ := setupIdentity(t)

	setDoc(k, ctx, "did:phi:a1", "ctrlOld", types.DID_STATUS_ACTIVE, 50)
	require.Equal(t, uint64(1), k.EligibleControllerTotal(ctx))
	require.True(t, k.IsEligibleControllerAt(ctx, "ctrlOld", time.Unix(50, 0), 0))

	setDoc(k, ctx, "did:phi:a1", "ctrlNew", types.DID_STATUS_ACTIVE, 50)

	require.False(t, k.IsEligibleControllerAt(ctx, "ctrlOld", time.Unix(50, 0), 0), "the old controller must lose eligibility")
	require.True(t, k.IsEligibleControllerAt(ctx, "ctrlNew", time.Unix(50, 0), 0), "the new controller must gain it")
	require.Equal(t, uint64(1), k.EligibleControllerTotal(ctx), "one human moved, the total must not change")
	require.Equal(t, uint64(1), countAt(k, ctx, 50))
	requireInvariantHolds(t, k, ctx)
}

// Recovery onto a controller that is ALREADY eligible must not double-count it, and must lower that controller's record if the arriving DID is older.
func TestEligibility_RecoveryOntoEligibleControllerDoesNotDoubleCount(t *testing.T) {
	ctx, k, _ := setupIdentity(t)

	setDoc(k, ctx, "did:phi:old", "ctrlOld", types.DID_STATUS_ACTIVE, 10)
	setDoc(k, ctx, "did:phi:new", "ctrlNew", types.DID_STATUS_ACTIVE, 500)
	require.Equal(t, uint64(2), k.EligibleControllerTotal(ctx))

	setDoc(k, ctx, "did:phi:old", "ctrlNew", types.DID_STATUS_ACTIVE, 10) // moves onto ctrlNew

	require.Equal(t, uint64(1), k.EligibleControllerTotal(ctx), "ctrlOld drops out; ctrlNew is not counted twice")
	require.Equal(t, uint64(1), countAt(k, ctx, 10), "ctrlNew now dates from the arriving older DID")
	requireInvariantHolds(t, k, ctx)
}

func requireInvariantHolds(t *testing.T, k keeper.Keeper, ctx sdk.Context) {
	t.Helper()
	msg, broken := keeper.EligibilityIndexInvariant(k)(ctx)
	require.False(t, broken, "eligibility invariant broken: %s", msg)
}

// Negative and zero created_at values must order correctly.
func TestEligibility_NegativeTimestampsOrderCorrectly(t *testing.T) {
	ctx, k, _ := setupIdentity(t)

	setDoc(k, ctx, "did:phi:neg", "ctrlNeg", types.DID_STATUS_ACTIVE, -500)
	setDoc(k, ctx, "did:phi:zero", "ctrlZero", types.DID_STATUS_ACTIVE, 0)
	setDoc(k, ctx, "did:phi:pos", "ctrlPos", types.DID_STATUS_ACTIVE, 500)

	require.Equal(t, uint64(0), countAt(k, ctx, -501))
	require.Equal(t, uint64(1), countAt(k, ctx, -500))
	require.Equal(t, uint64(2), countAt(k, ctx, 0))
	require.Equal(t, uint64(3), countAt(k, ctx, 500))
	requireInvariantHolds(t, k, ctx)
}

// Genesis must rebuild the derived structures exactly from the identity list alone.
func TestEligibility_GenesisRebuildsDerivedState(t *testing.T) {
	ctx, k, _ := setupIdentity(t)

	ctrlA := sdk.AccAddress([]byte("genesis_ctrl_A______")).String()
	ctrlB := sdk.AccAddress([]byte("genesis_ctrl_B______")).String()
	ctrlC := sdk.AccAddress([]byte("genesis_ctrl_C______")).String()

	genDoc := func(label, controller string, status types.DIDStatus, createdAt int64) {
		k.SetIdentity(ctx, types.DIDDocument{
			Did: didFor(label), Controller: controller, Status: status, CreatedAt: createdAt,
			PubKey: pubFor(label), UniquenessHash: []byte("uniq-" + label),
		})
	}
	genDoc("ga1", ctrlA, types.DID_STATUS_ACTIVE, 50)
	genDoc("ga2", ctrlA, types.DID_STATUS_ACTIVE, 150)
	genDoc("gb1", ctrlB, types.DID_STATUS_ACTIVE, 200)
	genDoc("gc1", ctrlC, types.DID_STATUS_REVOKED, 10)
	k.SetIdentityCount(ctx, 4)

	gs := k.ExportGenesis(ctx)

	var sawEligibilityRecord bool
	for _, e := range gs.StoreEntries {
		require.False(t, len(e.Key) > 0 && (e.Key[0] == types.EligibilityByAgePrefix[0] ||
			e.Key[0] == types.EligibleControllerTotalKey[0]),
			"purely derived eligibility state must not be exported: %x", e.Key)
		if len(e.Key) > 0 && e.Key[0] == types.ControllerEligibilityPrefix[0] {
			sawEligibilityRecord = true
		}
	}
	require.True(t, sawEligibilityRecord,
		"the eligibility record must be exported, or eligible_since is lost on every restart")

	ctx2, k2, _ := setupIdentity(t)
	k2.InitGenesis(ctx2, *gs)

	require.Equal(t, k.EligibleControllerTotal(ctx), k2.EligibleControllerTotal(ctx2))
	// export still admits a controller that was eligible then. Rebuilding eligible_since from the
	frozenAt := ctx.BlockTime()
	for _, controller := range []string{ctrlA, ctrlB} {
		require.True(t, k.IsEligibleControllerSince(ctx, controller, time.Unix(10_000, 0), 0, frozenAt),
			"precondition: %s is eligible against a basis frozen now", controller)
		require.True(t, k2.IsEligibleControllerSince(ctx2, controller, time.Unix(10_000, 0), 0, frozenAt),
			"controller %s lost its standing against a basis frozen before the restart", controller)
	}
	for _, cutoff := range []int64{0, 49, 50, 149, 150, 200, 10_000} {
		require.Equal(t, countAt(k, ctx, cutoff), countAt(k2, ctx2, cutoff), "cutoff %d", cutoff)
		require.Equal(t, referenceDenominator(t, k2, ctx2, cutoff), countAt(k2, ctx2, cutoff), "cutoff %d", cutoff)
	}
	requireInvariantHolds(t, k2, ctx2)
}

// The property test: apply a long random sequence of every transition that can reach SetIdentity — register, revoke, suspend, reinstate, rotate and social recovery (controller move) — and after EVERY step require the incremental structures to agree with a from-scratch recompute, at many cutoffs.
func TestEligibility_PropertyMatchesRecomputeAfterEveryTransition(t *testing.T) {
	const (
		steps       = 400
		controllers = 6
		dids        = 12
	)

	rng := rand.New(rand.NewSource(20260719))
	ctx, k, _ := setupIdentity(t)

	type docState struct {
		exists     bool
		controller string
		status     types.DIDStatus
		createdAt  int64
	}
	state := make([]docState, dids)
	statuses := []types.DIDStatus{types.DID_STATUS_ACTIVE, types.DID_STATUS_SUSPENDED, types.DID_STATUS_REVOKED}

	cutoffs := []int64{-1, 0, 1, 25, 50, 75, 100, 150, 1_000}

	for step := 0; step < steps; step++ {
		i := rng.Intn(dids)
		did := fmt.Sprintf("did:phi:d%02d", i)
		cur := state[i]

		switch {
		case !cur.exists:
			cur = docState{
				exists:     true,
				controller: fmt.Sprintf("ctrl%02d", rng.Intn(controllers)),
				status:     types.DID_STATUS_ACTIVE,
				createdAt:  int64(rng.Intn(120)),
			}
		default:
			switch rng.Intn(3) {
			case 0: // status transition (revoke / suspend / reinstate)
				cur.status = statuses[rng.Intn(len(statuses))]
			case 1: // key rotation: nothing eligibility-relevant changes
			case 2: // social recovery: the DID moves to another controller
				cur.controller = fmt.Sprintf("ctrl%02d", rng.Intn(controllers))
			}
		}
		state[i] = cur

		k.SetIdentity(ctx, types.DIDDocument{
			Did: did, Controller: cur.controller, Status: cur.status,
			CreatedAt: cur.createdAt, PubKey: []byte(fmt.Sprintf("pk-%d-%d", i, step)),
		})

		for _, cutoff := range cutoffs {
			require.Equal(t, referenceDenominator(t, k, ctx, cutoff), countAt(k, ctx, cutoff),
				"step %d (did %s): denominator disagrees with recompute at cutoff %d", step, did, cutoff)
		}

		for c := 0; c < controllers; c++ {
			controller := fmt.Sprintf("ctrl%02d", c)
			for _, cutoff := range cutoffs {
				want := referenceEligible(k, ctx, controller, cutoff)
				got := k.IsEligibleControllerAt(ctx, controller, time.Unix(cutoff, 0), 0)
				require.Equal(t, want, got,
					"step %d: eligibility of %s at cutoff %d disagrees", step, controller, cutoff)
			}
		}

		if msg, broken := keeper.EligibilityIndexInvariant(k)(ctx); broken {
			t.Fatalf("step %d (did %s): %s", step, did, msg)
		}
	}
}

func referenceEligible(k keeper.Keeper, ctx sdk.Context, controller string, cutoff int64) bool {
	found := false
	k.IterateIdentities(ctx, func(d types.DIDDocument) bool {
		if d.Controller == controller && d.Status != types.DID_STATUS_REVOKED && d.CreatedAt <= cutoff {
			found = true
		}
		return found
	})
	return found
}

// The denominator read must not walk the whole registry: its cost is the O(1) total minus a tail that spans only recently-onboarded controllers.
func TestEligibility_DenominatorCostIsFlatInRegistrySize(t *testing.T) {
	run := func(registry int) (uint64, uint64) {
		ctx, k, _ := setupIdentity(t)
		for i := 0; i < registry; i++ {
			setDoc(k, ctx, fmt.Sprintf("did:phi:bulk%06d", i), fmt.Sprintf("ctrlBulk%06d", i),
				types.DID_STATUS_ACTIVE, 1)
		}
		for i := 0; i < 5; i++ {
			setDoc(k, ctx, fmt.Sprintf("did:phi:recent%03d", i), fmt.Sprintf("ctrlRecent%03d", i),
				types.DID_STATUS_ACTIVE, 10_000)
		}

		ctx = ctx.WithGasMeter(storetypes.NewGasMeter(500_000_000))
		before := ctx.GasMeter().GasConsumed()
		got := countAt(k, ctx, 5_000)
		return ctx.GasMeter().GasConsumed() - before, got
	}

	smallGas, smallCount := run(50)
	largeGas, largeCount := run(1000)

	t.Logf("denominator gas: 50 identities = %d, 1000 identities = %d", smallGas, largeGas)

	require.Equal(t, uint64(50), smallCount)
	require.Equal(t, uint64(1000), largeCount)
	require.Equal(t, smallGas, largeGas,
		"denominator gas must not grow with registry size (%d vs %d identities)", 50, 1000)
}

// Sanity: the reference recompute and the sorted-snapshot formulation the old implementation used agree, so the property test is comparing against the original definition and not a restatement of the new one.
func TestEligibility_ReferenceMatchesSortedSnapshotDefinition(t *testing.T) {
	ctx, k, _ := setupIdentity(t)
	setDoc(k, ctx, "did:phi:a1", "ctrlA", types.DID_STATUS_ACTIVE, 10)
	setDoc(k, ctx, "did:phi:a2", "ctrlA", types.DID_STATUS_ACTIVE, 20)
	setDoc(k, ctx, "did:phi:b1", "ctrlB", types.DID_STATUS_ACTIVE, 30)
	setDoc(k, ctx, "did:phi:c1", "ctrlC", types.DID_STATUS_SUSPENDED, 5) // suspended still counts (in basis)

	oldest := map[string]int64{}
	k.IterateIdentities(ctx, func(d types.DIDDocument) bool {
		if d.Status != types.DID_STATUS_REVOKED {
			if cur, ok := oldest[d.Controller]; !ok || d.CreatedAt < cur {
				oldest[d.Controller] = d.CreatedAt
			}
		}
		return false
	})
	snap := make([]int64, 0, len(oldest))
	for _, v := range oldest {
		snap = append(snap, v)
	}
	sort.Slice(snap, func(i, j int) bool { return snap[i] < snap[j] })

	for _, cutoff := range []int64{0, 5, 9, 10, 20, 29, 30, 100} {
		want := uint64(sort.Search(len(snap), func(i int) bool { return snap[i] > cutoff }))
		require.Equal(t, want, countAt(k, ctx, cutoff), "cutoff %d", cutoff)
		require.Equal(t, want, referenceDenominator(t, k, ctx, cutoff), "cutoff %d", cutoff)
	}
}
