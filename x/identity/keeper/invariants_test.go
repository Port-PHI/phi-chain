// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"
	"time"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/phicrypto"
	"github.com/Port-PHI/phi-chain/x/identity/keeper"
	"github.com/Port-PHI/phi-chain/x/identity/types"
)

func setupIdentityWithKey(t *testing.T) (sdk.Context, keeper.Keeper, types.MsgServer, storetypes.StoreKey) {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_id_inv"))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	authority := sdk.AccAddress([]byte("gov_authority_______")).String()
	k := keeper.NewKeeper(cdc, key, authority, phicrypto.AcceptAll(), newFakeBank())
	ctx := testCtx.Ctx.WithBlockTime(time.Unix(1_000_000, 0))
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))
	k.SetTrustedIssuer(ctx, types.TrustedIssuer{Did: testIssuerDID, PubKey: []byte("issuer-pk"), Active: true})
	return ctx, k, keeper.NewMsgServerImpl(k), key
}

func requireInvariantsHold(t *testing.T, ctx sdk.Context, k keeper.Keeper, stage string) {
	t.Helper()
	if msg, broken := keeper.AllInvariants(k)(ctx); broken {
		t.Fatalf("invariant broken at stage %q: %s", stage, msg)
	}
}

// TestIdentityInvariants_NoBreakAcrossFullFlow drives every state-mutating message and asserts the invariants hold after each — including the case a naive check would trip on: two DIDs under one controller (a valid reachable state), which is why "no two identities share a controller" is NOT an invariant.
func TestIdentityInvariants_NoBreakAcrossFullFlow(t *testing.T) {
	ctx, k, msg, _ := setupIdentityWithKey(t)
	authority := k.GetAuthority()

	ctrlA := someAddr("controller-A________")
	ctrlB := someAddr("controller-B________")

	didA1 := registerActive(t, ctx, msg, ctrlA, "a1", []byte("bio-a1"))
	didA2 := registerActive(t, ctx, msg, ctrlA, "a2", []byte("bio-a2")) // shared controller — legal
	didB := registerActive(t, ctx, msg, ctrlB, "b1", []byte("bio-b1"))
	requireInvariantsHold(t, ctx, k, "after register (incl. shared controller)")

	if _, broken := keeper.ControllerIndexInvariant(k)(ctx); broken {
		t.Fatal("controller-index invariant false-positived on two DIDs sharing a controller")
	}

	_, err := msg.RotateIdentityKey(ctx, &types.MsgRotateIdentityKey{
		Creator: ctrlA, Did: didA1, NewPubKey: pubFor("a1-rotated"), PopSig: []byte("pop"),
	})
	require.NoError(t, err)
	requireInvariantsHold(t, ctx, k, "after rotate")

	_, err = msg.RevokeIdentity(ctx, &types.MsgRevokeIdentity{Creator: ctrlB, Did: didB})
	require.NoError(t, err)
	requireInvariantsHold(t, ctx, k, "after revoke")

	_, err = msg.UpdateStatus(ctx, &types.MsgUpdateStatus{
		Authority: authority, Did: didA2, NewStatus: types.DID_STATUS_SUSPENDED,
	})
	require.NoError(t, err)
	requireInvariantsHold(t, ctx, k, "after gov-suspend")

	_, err = msg.UpdateStatus(ctx, &types.MsgUpdateStatus{
		Authority: authority, Did: didA2, NewStatus: types.DID_STATUS_ACTIVE,
	})
	require.NoError(t, err)
	requireInvariantsHold(t, ctx, k, "after reinstate")

	_, commitments := guardianPool(t, ctx, msg, 3)
	_, err = msg.SetGuardians(ctx, &types.MsgSetGuardians{
		Controller: ctrlA, Did: didA1, Commitments: commitments, Threshold: 2,
	})
	require.NoError(t, err)
	requireInvariantsHold(t, ctx, k, "after set-guardians")
}

// TestIdentityInvariants_NoBreakAfterRecovery runs a full social-recovery flow (initiate → approve → execute) — which changes the controller of an existing DID — and asserts the invariants still hold.
func TestIdentityInvariants_NoBreakAfterRecovery(t *testing.T) {
	f := setupRecovery(t)
	requireInvariantsHold(t, f.ctx, f.k, "before recovery")

	id := f.initiate(t, recoveryNonce)
	requireInvariantsHold(t, f.ctx, f.k, "after initiate")

	f.approveToThreshold(t, id)
	f.warpPastWindow()

	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{
		Creator: someAddr("any-permissionless__"), RecoveryId: id,
	})
	require.NoError(t, err)
	requireInvariantsHold(t, f.ctx, f.k, "after execute-recovery (controller changed)")
}

func TestIdentityInvariants_DetectOrphanUniquenessMarker(t *testing.T) {
	ctx, k, msg, key := setupIdentityWithKey(t)
	registerActive(t, ctx, msg, someAddr("alice_______________"), "alice", []byte("bio-alice"))
	requireInvariantsHold(t, ctx, k, "clean")

	ctx.KVStore(key).Set(types.UniquenessKey([]byte("ghost-hash")), []byte("did:phi:ghost"))

	_, broken := keeper.UniquenessBijectionInvariant(k)(ctx)
	require.True(t, broken, "an orphan uniqueness marker must break the bijection")
}

func TestIdentityInvariants_DetectMissingUniquenessMarker(t *testing.T) {
	ctx, k, msg, key := setupIdentityWithKey(t)
	registerActive(t, ctx, msg, someAddr("alice_______________"), "alice", []byte("bio-alice"))

	ctx.KVStore(key).Delete(types.UniquenessKey([]byte("bio-alice")))

	_, broken := keeper.UniquenessBijectionInvariant(k)(ctx)
	require.True(t, broken, "an identity with no uniqueness marker must break the bijection")
}

func TestIdentityInvariants_DetectMispointedUniquenessMarker(t *testing.T) {
	ctx, k, msg, key := setupIdentityWithKey(t)
	registerActive(t, ctx, msg, someAddr("alice_______________"), "alice", []byte("bio-alice"))

	ctx.KVStore(key).Set(types.UniquenessKey([]byte("bio-alice")), []byte("did:phi:someone-else"))

	_, broken := keeper.UniquenessBijectionInvariant(k)(ctx)
	require.True(t, broken, "a mispointed uniqueness marker must break the bijection")
}

func TestIdentityInvariants_DetectDanglingControllerIndex(t *testing.T) {
	ctx, k, msg, key := setupIdentityWithKey(t)
	registerActive(t, ctx, msg, someAddr("alice_______________"), "alice", []byte("bio-alice"))
	requireInvariantsHold(t, ctx, k, "clean")

	ctx.KVStore(key).Set(types.ControllerIndexKey(someAddr("ghost-ctrl__________"), "did:phi:ghost"), []byte{1})

	_, broken := keeper.ControllerIndexInvariant(k)(ctx)
	require.True(t, broken, "a controller-index entry pointing to a missing DID must break the invariant")
}

func TestIdentityInvariants_DetectStaleControllerIndex(t *testing.T) {
	ctx, k, msg, key := setupIdentityWithKey(t)
	did := registerActive(t, ctx, msg, someAddr("alice_______________"), "alice", []byte("bio-alice"))

	ctx.KVStore(key).Set(types.ControllerIndexKey(someAddr("stale-ctrl___________"), did), []byte{1})

	_, broken := keeper.ControllerIndexInvariant(k)(ctx)
	require.True(t, broken, "a stale controller-index entry must break the invariant")
}

func TestIdentityInvariants_DetectInvalidStatus(t *testing.T) {
	ctx, k, msg, _ := setupIdentityWithKey(t)
	did := registerActive(t, ctx, msg, someAddr("alice_______________"), "alice", []byte("bio-alice"))
	requireInvariantsHold(t, ctx, k, "clean")

	doc, found := k.GetIdentity(ctx, did)
	require.True(t, found)
	doc.Status = types.DID_STATUS_UNSPECIFIED
	k.SetIdentity(ctx, doc)

	_, broken := keeper.StatusValidityInvariant(k)(ctx)
	require.True(t, broken, "an UNSPECIFIED status must break the status-validity invariant")
}
