// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"crypto/sha256"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/phicrypto"
	"github.com/Port-PHI/phi-chain/x/identity/types"
)

func recoveryCurveVerifier() phicrypto.Verifier {
	f := phicrypto.RejectAll()
	f.SignatureFn = func(curve phicrypto.Curve, publicKey, _, _ []byte) bool {
		switch len(publicKey) {
		case 33:
			return curve == phicrypto.Secp256k1
		case 65:
			return curve == phicrypto.Secp256r1
		default:
			return false
		}
	}
	return f
}

func k1PubFor(label string) []byte {
	scalar := sha256.Sum256([]byte("phi-test-k1-" + label))
	priv := secp256k1.PrivKeyFromBytes(scalar[:])
	return priv.PubKey().SerializeCompressed()
}

func k1DIDFor(t *testing.T, label string) string {
	t.Helper()
	did, err := types.DeriveDIDForKeyType(types.KEY_TYPE_SECP256K1, k1PubFor(label))
	require.NoError(t, err)
	return did
}

func setupK1Recovery(t *testing.T) *recoveryFixture {
	t.Helper()
	ctx, k, msg, bank := setupIdentityFull(t, recoveryCurveVerifier())
	now := time.Unix(1_000_000, 0)
	ctx = ctx.WithBlockTime(now)

	k.SetTrustedIssuer(ctx, types.TrustedIssuer{
		Did: testIssuerDID, PubKey: pubFor("k1-fixture-issuer"), Active: true,
	})

	oldCtrl := someAddr("k1-lost-seed-owner__")
	did := k1DIDFor(t, "k1-owner")
	_, err := msg.RegisterIdentity(ctx, &types.MsgRegisterIdentity{
		Creator: oldCtrl, Did: did, PubKey: k1PubFor("k1-owner"),
		UniquenessHash: []byte("bio-k1-owner"), KeyType: types.KEY_TYPE_SECP256K1,
		IssuerDid: testIssuerDID, IssuerSig: []byte("isig"),
		Nonce: []byte("nonce-k1-owner"), PopSig: []byte("pop-k1-owner"),
	})
	require.NoError(t, err)

	doc, found := k.GetIdentity(ctx, did)
	require.True(t, found)
	require.Equal(t, types.KEY_TYPE_SECP256K1, doc.KeyType, "the owner starts on k1")

	guardians, commitments := guardianPool(t, ctx, msg, 5)
	_, err = msg.SetGuardians(ctx, &types.MsgSetGuardians{
		Controller: oldCtrl, Did: did, Commitments: commitments, Threshold: 3,
	})
	require.NoError(t, err)

	newCtrl := someAddr("k1-new-device-acct__")
	deposit := k.GetParams(ctx).RecoveryDeposit()
	addr, err := sdk.AccAddressFromBech32(newCtrl)
	require.NoError(t, err)
	bank.Fund(addr, deposit.MulRaw(10))

	return &recoveryFixture{
		ctx: ctx, k: k, msg: msg, bank: bank,
		oldCtrl: oldCtrl, did: did, guardians: guardians,
		newCtrl: newCtrl, newKey: pubFor("k1-recovered-key"),
		deposit: deposit, now: now,
	}
}

// A k1 identity recovered by guardians ends up a valid r1 identity: the r1 key is installed AND key_type says r1, so the document describes the key it actually holds.
func TestRecovery_K1IdentityConvertsToR1(t *testing.T) {
	f := setupK1Recovery(t)

	id := f.initiate(t, recoveryNonce)
	f.approveToThreshold(t, id)
	f.warpPastWindow()
	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.NoError(t, err)

	doc, found := f.k.GetIdentity(f.ctx, f.did)
	require.True(t, found)
	require.Equal(t, f.newKey, doc.PubKey, "the r1 key is installed")
	require.Equal(t, types.KEY_TYPE_SECP256R1, doc.KeyType,
		"key_type must convert with the key — a stale k1 marker would describe a key that is not there")
	require.Equal(t, f.newCtrl, doc.Controller)

	curve, err := types.CurveForKeyType(doc.KeyType)
	require.NoError(t, err)
	require.Equal(t, phicrypto.Secp256r1, curve)

	require.Equal(t, f.did, doc.Did)
	require.Equal(t, []byte("bio-k1-owner"), doc.UniquenessHash)
}

// THE LOCKOUT.
func TestRecovery_RecoveredK1IdentityCanStillRotate(t *testing.T) {
	f := setupK1Recovery(t)

	id := f.initiate(t, recoveryNonce)
	f.approveToThreshold(t, id)
	f.warpPastWindow()
	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.NoError(t, err)

	_, err = f.msg.RotateIdentityKey(f.ctx, &types.MsgRotateIdentityKey{
		Creator: f.newCtrl, Did: f.did, NewPubKey: pubFor("k1-rotated-again"), PopSig: []byte("pop"),
	})
	require.NoError(t, err, "the recovered identity must not be locked out of its own key rotation")

	doc, found := f.k.GetIdentity(f.ctx, f.did)
	require.True(t, found)
	require.Equal(t, pubFor("k1-rotated-again"), doc.PubKey)
	require.Equal(t, types.KEY_TYPE_SECP256R1, doc.KeyType)
}

// An identity that was already on r1 keeps its curve: for it the conversion moves nothing.
func TestRecovery_R1IdentityKeepsItsCurve(t *testing.T) {
	f := setupRecovery(t)

	before, found := f.k.GetIdentity(f.ctx, f.did)
	require.True(t, found)
	curveBefore, err := types.CurveForKeyType(before.KeyType)
	require.NoError(t, err)

	id := f.initiate(t, recoveryNonce)
	f.approveToThreshold(t, id)
	f.warpPastWindow()
	_, err = f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.NoError(t, err)

	doc, found := f.k.GetIdentity(f.ctx, f.did)
	require.True(t, found)
	curveAfter, err := types.CurveForKeyType(doc.KeyType)
	require.NoError(t, err)
	require.Equal(t, curveBefore, curveAfter, "an r1 recovery changes no curve")
	require.Equal(t, phicrypto.Secp256r1, curveAfter)
	require.Equal(t, types.KEY_TYPE_SECP256R1, doc.KeyType, "and the curve is now named explicitly")
}
