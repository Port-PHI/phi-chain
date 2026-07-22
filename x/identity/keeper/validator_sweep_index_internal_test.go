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

func sortableDID(tag string, i int) string { return fmt.Sprintf("did:phi:%s-%05d", tag, i) }

func (k Keeper) recomputeSweep(ctx sdk.Context, controller string) (activeDID string, hasSuspended, hasRevoked, hasAny bool) {
	prefix := types.ControllerIndexPrefixFor(controller)
	store := ctx.KVStore(k.storeKey)
	it := storetypes.KVStorePrefixIterator(store, prefix)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		did := string(it.Key()[len(prefix):])
		d, found := k.GetIdentity(ctx, did)
		if !found {
			continue
		}
		hasAny = true
		switch d.Status {
		case types.DID_STATUS_ACTIVE:
			if activeDID == "" {
				activeDID = d.Did
			}
		case types.DID_STATUS_SUSPENDED:
			hasSuspended = true
		case types.DID_STATUS_REVOKED:
			hasRevoked = true
		}
	}
	return activeDID, hasSuspended, hasRevoked, hasAny
}

func requireSweepTracks(t *testing.T, k Keeper, ctx sdk.Context, controller string) {
	t.Helper()
	wantActive, wantSusp, wantRev, wantAny := k.recomputeSweep(ctx, controller)
	gotActive, gotSusp, gotRev, gotRecord := k.ControllerSweepStatus(ctx, controller)
	require.Equal(t, wantAny, gotRecord, "record presence must equal 'controls any DID'")
	require.Equal(t, wantActive, gotActive, "stored first-ACTIVE DID must equal the registry's")
	require.Equal(t, wantSusp, gotSusp, "stored has-SUSPENDED must equal the registry's")
	require.Equal(t, wantRev, gotRev, "stored has-REVOKED must equal the registry's")
}

func setStatus(k Keeper, ctx sdk.Context, did, controller string, createdAt int64, s types.DIDStatus) {
	k.SetIdentity(ctx, types.DIDDocument{Did: did, Controller: controller, Status: s, CreatedAt: createdAt, PubKey: []byte("pk")})
}

// TestSweepIndex_TracksEveryStatusTransition drives register → second DID → suspend → revoke → reinstate → rotate, and after each transition asserts the record equals a fresh recomputation.
func TestSweepIndex_TracksEveryStatusTransition(t *testing.T) {
	ctx, k := setupInternal(t)
	ctx = ctx.WithBlockTime(time.Unix(1_000_000, 0))
	const ctrl = "phi1sweeptrack"

	setStatus(k, ctx, "did:phi:a", ctrl, 100, types.DID_STATUS_ACTIVE)
	requireSweepTracks(t, k, ctx, ctrl)
	active, _, _, has := k.ControllerSweepStatus(ctx, ctrl)
	require.True(t, has)
	require.Equal(t, "did:phi:a", active)

	setStatus(k, ctx, "did:phi:b", ctrl, 200, types.DID_STATUS_ACTIVE)
	requireSweepTracks(t, k, ctx, ctrl)

	setStatus(k, ctx, "did:phi:a", ctrl, 100, types.DID_STATUS_SUSPENDED)
	requireSweepTracks(t, k, ctx, ctrl)
	active, susp, _, _ := k.ControllerSweepStatus(ctx, ctrl)
	require.Equal(t, "did:phi:b", active)
	require.True(t, susp)

	setStatus(k, ctx, "did:phi:b", ctrl, 200, types.DID_STATUS_REVOKED)
	requireSweepTracks(t, k, ctx, ctrl)
	active, susp, rev, _ := k.ControllerSweepStatus(ctx, ctrl)
	require.Equal(t, "", active)
	require.True(t, susp)
	require.True(t, rev)

	setStatus(k, ctx, "did:phi:a", ctrl, 100, types.DID_STATUS_ACTIVE)
	requireSweepTracks(t, k, ctx, ctrl)
	active, susp, rev, _ = k.ControllerSweepStatus(ctx, ctrl)
	require.Equal(t, "did:phi:a", active)
	require.False(t, susp)
	require.True(t, rev)

	k.SetIdentity(ctx, types.DIDDocument{Did: "did:phi:a", Controller: ctrl, Status: types.DID_STATUS_ACTIVE, CreatedAt: 100, PubKey: []byte("rotated")})
	requireSweepTracks(t, k, ctx, ctrl)
}

// TestSweepIndex_ControllerChangeRefreshesBothSides is the two-sided case social recovery produces: a DID moves from one controller to another, and BOTH records must be refreshed — the old controller loses its ACTIVE DID, the new one gains it.
func TestSweepIndex_ControllerChangeRefreshesBothSides(t *testing.T) {
	ctx, k := setupInternal(t)
	ctx = ctx.WithBlockTime(time.Unix(2_000_000, 0))
	const oldCtrl, newCtrl = "phi1oldctrl", "phi1newctrl"

	setStatus(k, ctx, "did:phi:moved", oldCtrl, 100, types.DID_STATUS_ACTIVE)
	requireSweepTracks(t, k, ctx, oldCtrl)

	oldActive, _, _, oldHas := k.ControllerSweepStatus(ctx, oldCtrl)
	require.True(t, oldHas)
	require.Equal(t, "did:phi:moved", oldActive)

	k.SetIdentity(ctx, types.DIDDocument{Did: "did:phi:moved", Controller: newCtrl, Status: types.DID_STATUS_ACTIVE, CreatedAt: 100, PubKey: []byte("pk")})
	requireSweepTracks(t, k, ctx, oldCtrl)
	requireSweepTracks(t, k, ctx, newCtrl)

	_, _, _, oldStillHas := k.ControllerSweepStatus(ctx, oldCtrl)
	require.False(t, oldStillHas, "the old controller no longer controls any DID")
	newActive, _, _, newHas := k.ControllerSweepStatus(ctx, newCtrl)
	require.True(t, newHas)
	require.Equal(t, "did:phi:moved", newActive)
}

// TestSweepIndex_HiddenActiveIsFoundRegardlessOfKeyOrder is the ordering-evasion attack at the keeper level: an ACTIVE DID placed far past where the old bounded scan would have stopped is still recorded as the operator's ACTIVE DID, so the sweep sees it.
func TestSweepIndex_HiddenActiveIsFoundRegardlessOfKeyOrder(t *testing.T) {
	ctx, k := setupInternal(t)
	ctx = ctx.WithBlockTime(time.Unix(3_000_000, 0))
	const ctrl = "phi1hidden"

	n := types.MaxControllerDIDScan * 3
	for i := range n {
		setStatus(k, ctx, sortableDID("aaa", i), ctrl, 100, types.DID_STATUS_REVOKED)
	}
	setStatus(k, ctx, "did:phi:zzz-active", ctrl, 100, types.DID_STATUS_ACTIVE)

	requireSweepTracks(t, k, ctx, ctrl)
	active, _, rev, _ := k.ControllerSweepStatus(ctx, ctrl)
	require.Equal(t, "did:phi:zzz-active", active, "the ACTIVE DID is found whatever its key position")
	require.True(t, rev)

	const gone = "phi1allrevoked"
	for i := range n {
		setStatus(k, ctx, sortableDID("rev", i), gone, 100, types.DID_STATUS_REVOKED)
	}
	requireSweepTracks(t, k, ctx, gone)
	active, susp, rev, has := k.ControllerSweepStatus(ctx, gone)
	require.True(t, has)
	require.Equal(t, "", active)
	require.False(t, susp)
	require.True(t, rev, "an all-revoked operator is terminal: the sweep tombstones it")
}
