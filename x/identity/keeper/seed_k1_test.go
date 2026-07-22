// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256r1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bip39 "github.com/cosmos/go-bip39"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/keeper"
	"github.com/Port-PHI/phi-chain/x/identity/types"
)

const (
	seedMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"
	seedPath     = "m/44'/118'/0'/0/0"
)

func deriveSeedKey(t *testing.T, mnemonic string) *secp256k1.PrivKey {
	t.Helper()
	seed, err := bip39.NewSeedWithErrorChecking(mnemonic, "")
	require.NoError(t, err)
	master, ch := hd.ComputeMastersFromSeed(seed)
	priv, err := hd.DerivePrivateKeyForPath(master, ch, seedPath)
	require.NoError(t, err)
	return &secp256k1.PrivKey{Key: priv}
}

type seedFixture struct {
	ctx    sdk.Context
	k      keeper.Keeper
	msg    types.MsgServer
	issuer *secp256r1.PrivKey
}

const seedIssuerDID = "did:phi:seed-path-issuer"

func setupSeed(t *testing.T) *seedFixture {
	t.Helper()
	ctx, k, msg, _ := setupIdentityFull(t, seedVerifier())
	ctx = ctx.WithBlockTime(time.Unix(1_000_000, 0))

	issuer, err := secp256r1.GenPrivKey()
	require.NoError(t, err)
	k.SetTrustedIssuer(ctx, types.TrustedIssuer{
		Did: seedIssuerDID, PubKey: issuer.PubKey().Bytes(), Active: true,
	})
	return &seedFixture{ctx: ctx, k: k, msg: msg, issuer: issuer}
}

func seedAttestationMessage(chainID, did string, pub, uniq []byte, creator, nonce string) []byte {
	return types.CanonicalMessage("phi-issuer-attestation-v3",
		[]byte(chainID), []byte(did), pub, uniq, []byte(creator), []byte(nonce))
}

func (f *seedFixture) registrationMsgForChain(t *testing.T, chainID string, priv *secp256k1.PrivKey, uniq []byte, nonce string) *types.MsgRegisterIdentity {
	t.Helper()
	pub := priv.PubKey().Bytes()
	did, err := types.DeriveDIDForKeyType(types.KEY_TYPE_SECP256K1, pub)
	require.NoError(t, err)
	creator := sdk.AccAddress(priv.PubKey().Address()).String()

	m := seedAttestationMessage(chainID, did, pub, uniq, creator, nonce)

	issuerSig, err := f.issuer.Sign(m) // r1 — the issuer's curve, never the registrant's
	require.NoError(t, err)
	popSig, err := priv.Sign(m) // k1 — the registrant's own curve
	require.NoError(t, err)

	return &types.MsgRegisterIdentity{
		Creator: creator, Did: did, PubKey: pub, UniquenessHash: uniq,
		IssuerDid: seedIssuerDID, IssuerSig: issuerSig,
		Nonce: []byte(nonce), PopSig: popSig,
		KeyType: types.KEY_TYPE_SECP256K1,
	}
}

func (f *seedFixture) registrationMsgFor(t *testing.T, priv *secp256k1.PrivKey, uniq []byte, nonce string) *types.MsgRegisterIdentity {
	t.Helper()
	return f.registrationMsgForChain(t, f.ctx.ChainID(), priv, uniq, nonce)
}

// A seed-derived k1 key registers as an ordinary identity: the DID is the canonical dual-curve k1 DID (tag 0x01 ‖ compressed SEC1), the curve is recorded, and the k1 account controls it.
func TestSeedK1_RegistersWithTheCanonicalK1DID(t *testing.T) {
	f := setupSeed(t)
	priv := deriveSeedKey(t, seedMnemonic)
	msg := f.registrationMsgFor(t, priv, []byte("uniqueness-marker-self-custody"), "seed-nonce-1")

	_, err := f.msg.RegisterIdentity(f.ctx, msg)
	require.NoError(t, err)

	require.Equal(t, "did:phi:17010867b1779053627535b78920d69733c4ef4725ad72c78948f1b14387472c", msg.Did)

	doc, found := f.k.GetIdentity(f.ctx, msg.Did)
	require.True(t, found)
	require.Equal(t, types.KEY_TYPE_SECP256K1, doc.KeyType, "the curve is part of the identity")
	require.Equal(t, priv.PubKey().Bytes(), doc.PubKey)
	require.Equal(t, types.DID_STATUS_ACTIVE, doc.Status)
	require.Equal(t, msg.Creator, doc.Controller)
	require.True(t, f.k.IsEligibleControllerAt(f.ctx, msg.Creator, f.ctx.BlockTime(), 0),
		"the k1 account controls the DID")
}

// THE RECOVERY ROUND-TRIP — the entire reason this path exists.
func TestSeedK1_DeviceLossReDeriveControlsTheSameDID(t *testing.T) {
	f := setupSeed(t)
	onOldDevice := deriveSeedKey(t, seedMnemonic)
	msg := f.registrationMsgFor(t, onOldDevice, []byte("uniqueness-marker-self-custody"), "seed-nonce-1")
	_, err := f.msg.RegisterIdentity(f.ctx, msg)
	require.NoError(t, err)

	before, found := f.k.GetIdentity(f.ctx, msg.Did)
	require.True(t, found)

	onNewDevice := deriveSeedKey(t, seedMnemonic)

	require.Equal(t, onOldDevice.Key, onNewDevice.Key, "same phrase → same secret key")
	require.Equal(t, onOldDevice.PubKey().Bytes(), onNewDevice.PubKey().Bytes())

	reDerivedDID, err := types.DeriveDIDForKeyType(types.KEY_TYPE_SECP256K1, onNewDevice.PubKey().Bytes())
	require.NoError(t, err)
	require.Equal(t, msg.Did, reDerivedDID, "the re-derived key names the SAME identity")

	after, found := f.k.GetIdentity(f.ctx, reDerivedDID)
	require.True(t, found)
	require.Equal(t, before, after, "recovery touched NO chain state whatsoever")
	require.Equal(t, onNewDevice.PubKey().Bytes(), after.PubKey)

	sig, err := onNewDevice.Sign([]byte("a transaction from the new device"))
	require.NoError(t, err)
	require.True(t, onNewDevice.PubKey().VerifySignature([]byte("a transaction from the new device"), sig),
		"the re-derived key signs as the identity's key")

	newCtrl := sdk.AccAddress(onNewDevice.PubKey().Address()).String()
	require.Equal(t, after.Controller, newCtrl, "and its account is still the controller")
	require.True(t, f.k.IsEligibleControllerAt(f.ctx, newCtrl, f.ctx.BlockTime(), 0))

	require.Empty(t, f.k.RecoveryRequestsForDID(f.ctx, msg.Did))
}

// A WRONG phrase is not a near miss: it derives a valid key to an identity that does not exist and controls nothing.
func TestSeedK1_WrongMnemonicControlsNothing(t *testing.T) {
	f := setupSeed(t)
	right := deriveSeedKey(t, seedMnemonic)
	msg := f.registrationMsgFor(t, right, []byte("uniqueness-marker-self-custody"), "seed-nonce-1")
	_, err := f.msg.RegisterIdentity(f.ctx, msg)
	require.NoError(t, err)

	entropy := make([]byte, 32)
	entropy[31] = 1
	wrongPhrase, err := bip39.NewMnemonic(entropy)
	require.NoError(t, err)
	wrong := deriveSeedKey(t, wrongPhrase)

	require.NotEqual(t, right.Key, wrong.Key)
	wrongDID, err := types.DeriveDIDForKeyType(types.KEY_TYPE_SECP256K1, wrong.PubKey().Bytes())
	require.NoError(t, err)
	require.NotEqual(t, msg.Did, wrongDID)

	require.False(t, f.k.HasIdentity(f.ctx, wrongDID))
	wrongCtrl := sdk.AccAddress(wrong.PubKey().Address()).String()
	require.False(t, f.k.IsEligibleControllerAt(f.ctx, wrongCtrl, f.ctx.BlockTime(), 0))

	doc, found := f.k.GetIdentity(f.ctx, msg.Did)
	require.True(t, found)
	require.Equal(t, right.PubKey().Bytes(), doc.PubKey)
	require.NotEqual(t, wrongCtrl, doc.Controller)
}

// CURVE CONFUSION.
func TestSeedK1_KeyTypeBindsTheCurve(t *testing.T) {
	f := setupSeed(t)
	priv := deriveSeedKey(t, seedMnemonic)
	uniq := []byte("uniqueness-marker-self-custody")

	mismatched := f.registrationMsgFor(t, priv, uniq, "seed-nonce-1")
	mismatched.KeyType = types.KEY_TYPE_SECP256R1
	_, err := f.msg.RegisterIdentity(f.ctx, mismatched)
	require.Error(t, err, "a k1 key must not register as an r1 identity")

	badPoP := f.registrationMsgFor(t, priv, uniq, "seed-nonce-2")
	impostor := deriveSeedKey(t, seedMnemonic) // same key…
	other := &secp256k1.PrivKey{Key: append([]byte{}, impostor.Key...)}
	other.Key[0] ^= 0xff // …perturbed into a different one
	sig, err := other.Sign([]byte("not the attestation message"))
	require.NoError(t, err)
	badPoP.PopSig = sig
	_, err = f.msg.RegisterIdentity(f.ctx, badPoP)
	require.ErrorIs(t, err, types.ErrInvalidPoP)

	unknown := f.registrationMsgFor(t, priv, uniq, "seed-nonce-3")
	unknown.KeyType = types.KeyType(99)
	require.Error(t, unknown.ValidateBasic(), "ValidateBasic rejects an unknown curve")
	_, err = f.msg.RegisterIdentity(f.ctx, unknown)
	require.Error(t, err)

	require.False(t, f.k.HasIdentity(f.ctx, mismatched.Did))
	require.False(t, f.k.HasUniqueness(f.ctx, uniq))
}

func seedRotationMessage(chainID, did string, newPub []byte, creator string) []byte {
	return types.CanonicalMessage("phi-key-rotation-v3",
		[]byte(chainID), []byte(did), newPub, []byte(creator))
}

// CROSS-CHAIN REPLAY — issuer attestation.
func TestSeedK1_IssuerAttestation_ChainBound(t *testing.T) {
	f := setupSeed(t)
	priv := deriveSeedKey(t, seedMnemonic)
	uniq := []byte("uniqueness-marker-self-custody")

	foreign := f.registrationMsgForChain(t, "phi-mainnet-1", priv, uniq, "seed-nonce-foreign")
	_, err := f.msg.RegisterIdentity(f.ctx, foreign)
	require.ErrorIs(t, err, types.ErrInvalidIssuerSig,
		"an attestation valid on another Phi chain must not verify here")
	require.False(t, f.k.HasIdentity(f.ctx, foreign.Did))
	require.False(t, f.k.HasUniqueness(f.ctx, uniq))

	local := f.registrationMsgForChain(t, f.ctx.ChainID(), priv, uniq, "seed-nonce-local")
	_, err = f.msg.RegisterIdentity(f.ctx, local)
	require.NoError(t, err)
	require.True(t, f.k.HasIdentity(f.ctx, local.Did))
}

// CROSS-CHAIN REPLAY — key-rotation proof-of-possession.
func TestSeedK1_RotationPoP_ChainBound(t *testing.T) {
	f := setupSeed(t)
	priv := deriveSeedKey(t, seedMnemonic)
	uniq := []byte("uniqueness-marker-self-custody")
	reg := f.registrationMsgFor(t, priv, uniq, "seed-nonce-1")
	_, err := f.msg.RegisterIdentity(f.ctx, reg)
	require.NoError(t, err)
	did, ctrl := reg.Did, reg.Creator

	newPriv := secp256k1.GenPrivKey() // a fresh k1 key to rotate onto
	newPub := newPriv.PubKey().Bytes()

	foreignSig, err := newPriv.Sign(seedRotationMessage("phi-mainnet-1", did, newPub, ctrl))
	require.NoError(t, err)
	_, err = f.msg.RotateIdentityKey(f.ctx, &types.MsgRotateIdentityKey{
		Creator: ctrl, Did: did, NewPubKey: newPub, PopSig: foreignSig,
	})
	require.ErrorIs(t, err, types.ErrInvalidPoP,
		"a rotation proof-of-possession valid on another Phi chain must not verify here")
	doc, found := f.k.GetIdentity(f.ctx, did)
	require.True(t, found)
	require.Equal(t, priv.PubKey().Bytes(), doc.PubKey, "the key was NOT rotated")

	localSig, err := newPriv.Sign(seedRotationMessage(f.ctx.ChainID(), did, newPub, ctrl))
	require.NoError(t, err)
	_, err = f.msg.RotateIdentityKey(f.ctx, &types.MsgRotateIdentityKey{
		Creator: ctrl, Did: did, NewPubKey: newPub, PopSig: localSig,
	})
	require.NoError(t, err)
	doc, found = f.k.GetIdentity(f.ctx, did)
	require.True(t, found)
	require.Equal(t, newPub, doc.PubKey, "the key rotated")
}

// NO REGRESSION on the default path: an r1 passkey identity registers exactly as before, including when the client omits key_type entirely (the zero value must keep meaning "P-256 passkey").
func TestSeedK1_R1DefaultPathUnchanged(t *testing.T) {
	f := setupSeed(t)

	r1Priv, err := secp256r1.GenPrivKey()
	require.NoError(t, err)
	pub := r1Priv.PubKey().Bytes()
	did, err := types.DeriveDIDFromP256(pub)
	require.NoError(t, err)
	creator := sdk.AccAddress(r1Priv.PubKey().Address()).String()
	uniq := []byte("uniqueness-marker-passkey-user")

	m := seedAttestationMessage(f.ctx.ChainID(), did, pub, uniq, creator, "r1-nonce")
	issuerSig, err := f.issuer.Sign(m)
	require.NoError(t, err)
	popSig, err := r1Priv.Sign(m)
	require.NoError(t, err)

	_, err = f.msg.RegisterIdentity(f.ctx, &types.MsgRegisterIdentity{
		Creator: creator, Did: did, PubKey: pub, UniquenessHash: uniq,
		IssuerDid: seedIssuerDID, IssuerSig: issuerSig,
		Nonce: []byte("r1-nonce"), PopSig: popSig,
	})
	require.NoError(t, err, "an omitted key_type must still mean P-256 passkey")

	doc, found := f.k.GetIdentity(f.ctx, did)
	require.True(t, found)
	require.Equal(t, types.KEY_TYPE_UNSPECIFIED, doc.KeyType, "stored as the zero value, read as r1")
	require.Equal(t, types.DID_STATUS_ACTIVE, doc.Status)
}
