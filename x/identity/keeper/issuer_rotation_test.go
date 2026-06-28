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

// --- Item 5: trusted issuer + PoP ---

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
	// RejectAll models the default build's fail-closed Disabled port without depending on the build tag.
	ctx, _, msg := setupIdentityV(t, phicrypto.RejectAll())
	_, err := msg.RegisterIdentity(ctx, reg(someAddr("u___________________"), "x", []byte("bio-x")))
	require.ErrorIs(t, err, types.ErrInvalidIssuerSig)
}

func TestRegisterIdentity_PoPFailsClosed(t *testing.T) {
	// Accept the issuer attestation but reject the proof-of-possession (distinguished by the message:
	// only the registrant's PoP is signed with pub_key — but both use the same attestation message, so
	// here we model a verifier that accepts only the issuer key and rejects pub_key).
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

	// Non-authority rejected.
	_, err := msg.RegisterTrustedIssuer(ctx, &types.MsgRegisterTrustedIssuer{Authority: someAddr("notgov_____________"), Issuer: iss})
	require.Error(t, err)

	// Authority registers + revokes.
	_, err = msg.RegisterTrustedIssuer(ctx, &types.MsgRegisterTrustedIssuer{Authority: authority, Issuer: iss})
	require.NoError(t, err)
	require.True(t, k.IsTrustedIssuer(ctx, "did:phi:iss2"))

	_, err = msg.RevokeTrustedIssuer(ctx, &types.MsgRevokeTrustedIssuer{Authority: authority, Did: "did:phi:iss2"})
	require.NoError(t, err)
	require.False(t, k.IsTrustedIssuer(ctx, "did:phi:iss2"))

	// Revoking an unknown issuer errors.
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

// --- Item 6: rotation + secondary index ---

func TestRotateIdentityKey_HappyPath(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	now := time.Unix(10_000_000, 0)
	ctx = ctx.WithBlockTime(now)
	ctrl := someAddr("ctrl________________")
	_, err := msg.RegisterIdentity(ctx, reg(ctrl, "a", []byte("bio-a")))
	require.NoError(t, err)
	did := didFor("a")

	newPub := []byte("rotated-pubkey")
	_, err = msg.RotateIdentityKey(ctx, &types.MsgRotateIdentityKey{Creator: ctrl, Did: did, NewPubKey: newPub, PopSig: []byte("pop")})
	require.NoError(t, err)

	doc, ok := k.GetIdentity(ctx, did)
	require.True(t, ok)
	require.Equal(t, newPub, doc.PubKey)                  // key rotated
	require.Equal(t, ctrl, doc.Controller)                // controller preserved
	require.Equal(t, []byte("bio-a"), doc.UniquenessHash) // uniqueness preserved
	// Eligibility (controller index) still resolves after rotation.
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

func TestRotateIdentityKey_PoPFailsClosed(t *testing.T) {
	// Accept registration attestation but reject the rotation proof-of-possession (distinguished by domain).
	verifier := phicrypto.Fake{SignatureFn: func(_ phicrypto.Curve, _, msg, _ []byte) bool {
		return !bytes.HasPrefix(msg, []byte("phi-key-rotation-v1"))
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

	// Fresh keeper (no pre-seed) + InitGenesis → index and issuers reconstructed.
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_id2"))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	k2 := keeper.NewKeeper(cdc, key, k.GetAuthority(), phicrypto.AcceptAll())
	ctx2 := testCtx.Ctx.WithBlockTime(now)
	k2.InitGenesis(ctx2, *exported)

	require.True(t, k2.IsTrustedIssuer(ctx2, testIssuerDID))
	// Controller eligibility resolves purely from the rebuilt index.
	require.True(t, k2.IsEligibleControllerAt(ctx2, ctrl, now.Add(8*24*time.Hour), k2.MinIdentityAge(ctx2)))
}

// A genesis exported AFTER a key rotation must pass its own ValidateGenesis and round-trip through a
// fresh InitGenesis without panicking. RotateIdentityKey keeps the DID stable while replacing pub_key, so
// the exported identity has did != DeriveDIDFromP256(pub_key); genesis Validate must accept that (the
// anti-Sybil anchor is the uniqueness-marker ↔ DID relation, not pubkey self-cert). Previously the
// unconditional self-cert check made InitGenesis panic on any rotated identity.
func TestGenesis_RotatedDIDRoundTrips(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	now := time.Unix(10_000_000, 0)
	ctx = ctx.WithBlockTime(now)
	ctrl := someAddr("ctrl________________")
	_, err := msg.RegisterIdentity(ctx, reg(ctrl, "a", []byte("bio-a")))
	require.NoError(t, err)
	did := didFor("a")

	// Rotate the passkey: DID + controller + uniqueness preserved, pub_key replaced.
	rotated := []byte("rotated-pubkey-not-self-certifying")
	_, err = msg.RotateIdentityKey(ctx, &types.MsgRotateIdentityKey{Creator: ctrl, Did: did, NewPubKey: rotated, PopSig: []byte("pop")})
	require.NoError(t, err)
	require.NotEqual(t, did, types.DeriveDIDFromP256(rotated), "precondition: the rotated identity no longer self-certifies")

	// Export → the exported genesis must validate even though the identity was rotated.
	exported := k.ExportGenesis(ctx)
	require.NoError(t, exported.Validate(), "ExportGenesis of a rotated identity must pass its own ValidateGenesis")

	// Round-trip into a fresh keeper: InitGenesis must not panic and must reproduce identical state.
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_id_rot"))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	k2 := keeper.NewKeeper(cdc, key, k.GetAuthority(), phicrypto.AcceptAll())
	ctx2 := testCtx.Ctx.WithBlockTime(now)
	require.NotPanics(t, func() { k2.InitGenesis(ctx2, *exported) })

	// State reproduced: the rotated pub_key, preserved controller + uniqueness, and counter all survive.
	doc, ok := k2.GetIdentity(ctx2, did)
	require.True(t, ok)
	require.Equal(t, rotated, doc.PubKey)
	require.Equal(t, ctrl, doc.Controller)
	require.Equal(t, []byte("bio-a"), doc.UniquenessHash)
	require.Equal(t, k.GetIdentityCount(ctx), k2.GetIdentityCount(ctx2))
	// A second export reproduces the first byte-for-byte (idempotent round-trip of the rotated state).
	require.Equal(t, exported, k2.ExportGenesis(ctx2))
}
