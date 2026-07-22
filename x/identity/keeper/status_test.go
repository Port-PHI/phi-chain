// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

func registerActive(t *testing.T, ctx sdk.Context, msg types.MsgServer, ctrl, label string, uniq []byte) string {
	t.Helper()
	_, err := msg.RegisterIdentity(ctx, reg(ctrl, label, uniq))
	require.NoError(t, err)
	return didFor(label)
}

func requireStatusChanged(t *testing.T, ctx sdk.Context, did, oldStatus, newStatus string) {
	t.Helper()
	for _, e := range ctx.EventManager().Events() {
		if e.Type != types.EventTypeStatusChanged {
			continue
		}
		got := map[string]string{}
		for _, a := range e.Attributes {
			got[a.Key] = a.Value
		}
		require.Equal(t, did, got[types.AttributeKeyDID])
		require.Equal(t, oldStatus, got[types.AttributeKeyOldStatus])
		require.Equal(t, newStatus, got[types.AttributeKeyNewStatus])
		return
	}
	t.Fatalf("StatusChanged event was not emitted")
}

// Governance suspends an ACTIVE DID and later reinstates it; StatusChanged is emitted on each transition.
func TestUpdateStatus_SuspendThenReinstate(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	now := time.Unix(1_000_000, 0)
	ctx = ctx.WithBlockTime(now)
	auth := k.GetAuthority()
	ctrl := someAddr("alice_______________")
	did := registerActive(t, ctx, msg, ctrl, "alice", []byte("bio-alice"))

	require.True(t, k.IsEligibleControllerAt(ctx, ctrl, now, 0), "an active DID is vote-eligible")

	ctx = ctx.WithEventManager(sdk.NewEventManager())
	_, err := msg.UpdateStatus(ctx, &types.MsgUpdateStatus{Authority: auth, Did: did, NewStatus: types.DID_STATUS_SUSPENDED})
	require.NoError(t, err)
	doc, found := k.GetIdentity(ctx, did)
	require.True(t, found)
	require.Equal(t, types.DID_STATUS_SUSPENDED, doc.Status)
	requireStatusChanged(t, ctx, did, "DID_STATUS_ACTIVE", "DID_STATUS_SUSPENDED")

	require.True(t, k.HasNonActiveDID(ctx, ctrl), "a suspended controller is non-active: the ante blocks its votes")
	require.True(t, k.IsEligibleControllerAt(ctx, ctrl, now, 0),
		"a suspended DID keeps its standing in the eligibility basis")

	ctx = ctx.WithEventManager(sdk.NewEventManager())
	_, err = msg.UpdateStatus(ctx, &types.MsgUpdateStatus{Authority: auth, Did: did, NewStatus: types.DID_STATUS_ACTIVE})
	require.NoError(t, err)
	doc, _ = k.GetIdentity(ctx, did)
	require.Equal(t, types.DID_STATUS_ACTIVE, doc.Status)
	requireStatusChanged(t, ctx, did, "DID_STATUS_SUSPENDED", "DID_STATUS_ACTIVE")
	require.False(t, k.HasNonActiveDID(ctx, ctrl), "a reinstated controller is active again")
	require.True(t, k.IsEligibleControllerAt(ctx, ctrl, now, 0), "a reinstated DID can vote again")
}

// A non-governance signer cannot change status.
func TestUpdateStatus_NonAuthorityRejected(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	ctx = ctx.WithBlockTime(time.Unix(1_000_000, 0))
	ctrl := someAddr("bob_________________")
	did := registerActive(t, ctx, msg, ctrl, "bob", []byte("bio-bob"))
	require.NotEqual(t, ctrl, k.GetAuthority())

	_, err := msg.UpdateStatus(ctx, &types.MsgUpdateStatus{Authority: ctrl, Did: did, NewStatus: types.DID_STATUS_SUSPENDED})
	require.ErrorIs(t, err, govtypes.ErrInvalidSigner)
}

// REVOKED is terminal: UpdateStatus can neither set REVOKED nor leave it; MsgRevokeIdentity is the revoke path and its signer is then blocked by the status guard.
func TestUpdateStatus_RevokedTerminal(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	ctx = ctx.WithBlockTime(time.Unix(1_000_000, 0))
	auth := k.GetAuthority()
	ctrl := someAddr("carol_______________")
	did := registerActive(t, ctx, msg, ctrl, "carol", []byte("bio-carol"))

	_, err := msg.UpdateStatus(ctx, &types.MsgUpdateStatus{Authority: auth, Did: did, NewStatus: types.DID_STATUS_REVOKED})
	require.ErrorIs(t, err, types.ErrInvalidStatusTransition)

	_, err = msg.RevokeIdentity(ctx, &types.MsgRevokeIdentity{Creator: ctrl, Did: did})
	require.NoError(t, err)
	doc, _ := k.GetIdentity(ctx, did)
	require.Equal(t, types.DID_STATUS_REVOKED, doc.Status)
	require.True(t, k.HasNonActiveDID(ctx, ctrl), "a revoked controller is non-active (guard blocks it)")

	_, err = msg.UpdateStatus(ctx, &types.MsgUpdateStatus{Authority: auth, Did: did, NewStatus: types.DID_STATUS_ACTIVE})
	require.ErrorIs(t, err, types.ErrIdentityRevoked)
	_, err = msg.UpdateStatus(ctx, &types.MsgUpdateStatus{Authority: auth, Did: did, NewStatus: types.DID_STATUS_SUSPENDED})
	require.ErrorIs(t, err, types.ErrIdentityRevoked)
}

// A no-op transition (already in the target status) is rejected, and an unknown DID is not found.
func TestUpdateStatus_NoOpAndNotFound(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	ctx = ctx.WithBlockTime(time.Unix(1_000_000, 0))
	auth := k.GetAuthority()
	ctrl := someAddr("dave________________")
	did := registerActive(t, ctx, msg, ctrl, "dave", []byte("bio-dave"))

	_, err := msg.UpdateStatus(ctx, &types.MsgUpdateStatus{Authority: auth, Did: did, NewStatus: types.DID_STATUS_ACTIVE})
	require.ErrorIs(t, err, types.ErrInvalidStatusTransition, "already ACTIVE is not a transition")

	_, err = msg.UpdateStatus(ctx, &types.MsgUpdateStatus{Authority: auth, Did: didFor("ghost"), NewStatus: types.DID_STATUS_SUSPENDED})
	require.ErrorIs(t, err, types.ErrIdentityNotFound)
}

// An account with no DID (an institution/validator operator) is never flagged by the status guard.
func TestHasNonActiveDID_NoDIDAccountPasses(t *testing.T) {
	ctx, k, _ := setupIdentity(t)
	require.False(t, k.HasNonActiveDID(ctx, someAddr("institution-operator")), "a no-DID account is not flagged")
}

// A SUSPENDED DID round-trips through ExportGenesis/InitGenesis with its status preserved.
func TestGenesis_SuspendedRoundTrip(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	ctx = ctx.WithBlockTime(time.Unix(1_000_000, 0)).WithBlockHeight(1)
	auth := k.GetAuthority()
	ctrl := someAddr("erin________________")
	did := registerActive(t, ctx, msg, ctrl, "erin", []byte("bio-erin"))
	_, err := msg.UpdateStatus(ctx, &types.MsgUpdateStatus{Authority: auth, Did: did, NewStatus: types.DID_STATUS_SUSPENDED})
	require.NoError(t, err)

	gs := k.ExportGenesis(ctx)
	require.NoError(t, gs.Validate(), "genesis with a SUSPENDED DID must validate")

	ctx2, k2, _ := setupIdentity(t)
	k2.InitGenesis(ctx2, *gs)
	doc, found := k2.GetIdentity(ctx2, did)
	require.True(t, found)
	require.Equal(t, types.DID_STATUS_SUSPENDED, doc.Status)
}
