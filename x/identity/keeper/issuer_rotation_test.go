// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"bytes"
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

func someAddr(s string) string { return sdk.AccAddress([]byte(s)).String() }

func TestRegisterIdentity_RejectsUntrustedIssuer(t *testing.T) {
	ctx, _, msg := setupIdentity(t)
	m := reg(someAddr("u___________________"), "x", []byte("bio-x"))
	m.IssuerDid = "did:phi:ghost"
	_, err := msg.RegisterIdentity(ctx, m)
	require.ErrorIs(t, err, types.ErrIssuerNotTrusted)
}

func TestRegisterIdentity_RejectsNonDerivedDID(t *testing.T) {
	ctx, _, msg := setupIdentity(t)
	m := reg(someAddr("u___________________"), "x", []byte("bio-x"))
	m.Did = "did:phi:not-the-derivation"
	_, err := msg.RegisterIdentity(ctx, m)
	require.ErrorIs(t, err, types.ErrInvalidDID)
}

func TestRegisterIdentity_FailsClosedWithDefaultVerifier(t *testing.T) {
	ctx, _, msg := setupIdentityV(t, phicrypto.RejectAll())
	_, err := msg.RegisterIdentity(ctx, reg(someAddr("u___________________"), "x", []byte("bio-x")))
	require.ErrorIs(t, err, types.ErrInvalidIssuerSig)
}

func TestRegisterIdentity_PoPFailsClosed(t *testing.T) {
	verifier := phicrypto.Fake{SignatureFn: func(_ phicrypto.Curve, pk, _, _ []byte) bool {
		return bytes.Equal(pk, []byte("issuer-pk"))
	}}
	ctx, _, msg := setupIdentityV(t, verifier)
	_, err := msg.RegisterIdentity(ctx, reg(someAddr("u___________________"), "x", []byte("bio-x")))
	require.ErrorIs(t, err, types.ErrInvalidPoP)
}

func TestTrustedIssuer_RegisterRevoke_GovOnly(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	authority := k.GetAuthority()
	iss := types.TrustedIssuer{Did: "did:phi:iss2", PubKey: []byte("k2"), Active: true}

	_, err := msg.RegisterTrustedIssuer(ctx, &types.MsgRegisterTrustedIssuer{Authority: someAddr("notgov_____________"), Issuer: iss})
	require.Error(t, err)

	_, err = msg.RegisterTrustedIssuer(ctx, &types.MsgRegisterTrustedIssuer{Authority: authority, Issuer: iss})
	require.NoError(t, err)
	require.True(t, k.IsTrustedIssuer(ctx, "did:phi:iss2"))

	_, err = msg.RevokeTrustedIssuer(ctx, &types.MsgRevokeTrustedIssuer{Authority: authority, Did: "did:phi:iss2"})
	require.NoError(t, err)
	require.False(t, k.IsTrustedIssuer(ctx, "did:phi:iss2"))

	_, err = msg.RevokeTrustedIssuer(ctx, &types.MsgRevokeTrustedIssuer{Authority: authority, Did: "did:phi:nope"})
	require.ErrorIs(t, err, types.ErrIssuerNotFound)
}

func TestRegisterIdentity_RevokedIssuerRejected(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	_, err := msg.RevokeTrustedIssuer(ctx, &types.MsgRevokeTrustedIssuer{Authority: k.GetAuthority(), Did: testIssuerDID})
	require.NoError(t, err)
	_, err = msg.RegisterIdentity(ctx, reg(someAddr("u___________________"), "x", []byte("bio-x")))
	require.ErrorIs(t, err, types.ErrIssuerNotTrusted)
}

func TestRotateIdentityKey_HappyPath(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	now := time.Unix(10_000_000, 0)
	ctx = ctx.WithBlockTime(now)
	ctrl := someAddr("ctrl________________")
	_, err := msg.RegisterIdentity(ctx, reg(ctrl, "a", []byte("bio-a")))
	require.NoError(t, err)
	did := didFor("a")

	newPub := pubFor("rotated-a")
	_, err = msg.RotateIdentityKey(ctx, &types.MsgRotateIdentityKey{Creator: ctrl, Did: did, NewPubKey: newPub, PopSig: []byte("pop")})
	require.NoError(t, err)

	doc, ok := k.GetIdentity(ctx, did)
	require.True(t, ok)
	require.Equal(t, newPub, doc.PubKey)                  // key rotated
	require.Equal(t, ctrl, doc.Controller)                // controller preserved
	require.Equal(t, []byte("bio-a"), doc.UniquenessHash) // uniqueness preserved
	require.True(t, k.IsEligibleControllerAt(ctx, ctrl, now.Add(8*24*time.Hour), k.MinIdentityAge(ctx)))
}

func TestRotateIdentityKey_OnlyController(t *testing.T) {
	ctx, _, msg := setupIdentity(t)
	ctrl := someAddr("ctrl________________")
	_, err := msg.RegisterIdentity(ctx, reg(ctrl, "a", []byte("bio-a")))
	require.NoError(t, err)
	_, err = msg.RotateIdentityKey(ctx, &types.MsgRotateIdentityKey{
		Creator: someAddr("attacker____________"), Did: didFor("a"), NewPubKey: []byte("evil"), PopSig: []byte("pop"),
	})
	require.ErrorIs(t, err, types.ErrUnauthorized)
}

// Rotation enforces the same key-collision invariant as recovery: the new key must not already self-certify ANOTHER registered identity.
func TestRotateIdentityKey_RejectsKeyCollidingWithAnotherIdentity(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	ctrlA := someAddr("ctrl-a______________")
	ctrlB := someAddr("ctrl-b______________")
	_, err := msg.RegisterIdentity(ctx, reg(ctrlA, "a", []byte("bio-a")))
	require.NoError(t, err)
	_, err = msg.RegisterIdentity(ctx, reg(ctrlB, "b", []byte("bio-b")))
	require.NoError(t, err)

	_, err = msg.RotateIdentityKey(ctx, &types.MsgRotateIdentityKey{
		Creator: ctrlA, Did: didFor("a"), NewPubKey: pubFor("b"), PopSig: []byte("pop"),
	})
	require.ErrorIs(t, err, types.ErrRecoveryKeyCollision, "a key self-certifying another DID must be refused")

	doc, ok := k.GetIdentity(ctx, didFor("a"))
	require.True(t, ok)
	require.Equal(t, pubFor("a"), doc.PubKey, "the rejected rotation must not have replaced the key")

	_, err = msg.RotateIdentityKey(ctx, &types.MsgRotateIdentityKey{
		Creator: ctrlA, Did: didFor("a"), NewPubKey: pubFor("a-fresh"), PopSig: []byte("pop"),
	})
	require.NoError(t, err)
	_, err = msg.RotateIdentityKey(ctx, &types.MsgRotateIdentityKey{
		Creator: ctrlA, Did: didFor("a"), NewPubKey: pubFor("a"), PopSig: []byte("pop"),
	})
	require.NoError(t, err, "rotating back onto the identity's own original key must stay permitted")
}

func TestRotateIdentityKey_PoPFailsClosed(t *testing.T) {
	rotationPrefix := types.CanonicalMessage("phi-key-rotation-v3")
	verifier := phicrypto.Fake{SignatureFn: func(_ phicrypto.Curve, _, msg, _ []byte) bool {
		return !bytes.HasPrefix(msg, rotationPrefix)
	}}
	ctx, _, msg := setupIdentityV(t, verifier)
	ctrl := someAddr("ctrl________________")
	_, err := msg.RegisterIdentity(ctx, reg(ctrl, "a", []byte("bio-a")))
	require.NoError(t, err)
	_, err = msg.RotateIdentityKey(ctx, &types.MsgRotateIdentityKey{Creator: ctrl, Did: didFor("a"), NewPubKey: []byte("new"), PopSig: []byte("pop")})
	require.ErrorIs(t, err, types.ErrInvalidPoP)
}

// InitGenesis rebuilds the controller→DID secondary index and the trusted-issuer registry.
func TestIdentity_GenesisRebuildsIndexAndIssuers(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	now := time.Unix(10_000_000, 0)
	ctx = ctx.WithBlockTime(now)
	ctrl := someAddr("ctrl________________")
	_, err := msg.RegisterIdentity(ctx, reg(ctrl, "a", []byte("bio-a")))
	require.NoError(t, err)

	exported := k.ExportGenesis(ctx)
	require.NoError(t, exported.Validate())
	require.Len(t, exported.TrustedIssuers, 1)
	require.Len(t, exported.Identities, 1)

	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_id2"))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	k2 := keeper.NewKeeper(cdc, key, k.GetAuthority(), phicrypto.AcceptAll(), newFakeBank())
	ctx2 := testCtx.Ctx.WithBlockTime(now)
	k2.InitGenesis(ctx2, *exported)

	require.True(t, k2.IsTrustedIssuer(ctx2, testIssuerDID))
	require.True(t, k2.IsEligibleControllerAt(ctx2, ctrl, now.Add(8*24*time.Hour), k2.MinIdentityAge(ctx2)))
}

// A genesis exported AFTER a key rotation must pass its own ValidateGenesis and round-trip through a fresh InitGenesis without panicking.
func TestGenesis_RotatedDIDRoundTrips(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	now := time.Unix(10_000_000, 0)
	ctx = ctx.WithBlockTime(now)
	ctrl := someAddr("ctrl________________")
	_, err := msg.RegisterIdentity(ctx, reg(ctrl, "a", []byte("bio-a")))
	require.NoError(t, err)
	did := didFor("a")

	rotated := pubFor("rotated")
	_, err = msg.RotateIdentityKey(ctx, &types.MsgRotateIdentityKey{Creator: ctrl, Did: did, NewPubKey: rotated, PopSig: []byte("pop")})
	require.NoError(t, err)
	selfCert, err := types.DeriveDIDFromP256(rotated)
	require.NoError(t, err)
	require.NotEqual(t, did, selfCert, "precondition: the rotated identity no longer self-certifies")

	exported := k.ExportGenesis(ctx)
	require.NoError(t, exported.Validate(), "ExportGenesis of a rotated identity must pass its own ValidateGenesis")

	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_id_rot"))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	k2 := keeper.NewKeeper(cdc, key, k.GetAuthority(), phicrypto.AcceptAll(), newFakeBank())
	ctx2 := testCtx.Ctx.WithBlockTime(now)
	require.NotPanics(t, func() { k2.InitGenesis(ctx2, *exported) })

	doc, ok := k2.GetIdentity(ctx2, did)
	require.True(t, ok)
	require.Equal(t, rotated, doc.PubKey)
	require.Equal(t, ctrl, doc.Controller)
	require.Equal(t, []byte("bio-a"), doc.UniquenessHash)
	require.Equal(t, k.GetIdentityCount(ctx), k2.GetIdentityCount(ctx2))
	require.Equal(t, exported, k2.ExportGenesis(ctx2))
}
