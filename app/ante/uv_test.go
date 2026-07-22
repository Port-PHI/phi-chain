// SPDX-License-Identifier: Apache-2.0

package ante_test

import (
	"bytes"
	"testing"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256r1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/stretchr/testify/require"

	phiante "github.com/Port-PHI/phi-chain/app/ante"
	"github.com/Port-PHI/phi-chain/phicrypto"
	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	identitytypes "github.com/Port-PHI/phi-chain/x/identity/types"
)

// WebAuthn authenticatorData flag bits (mirrors phi-crypto src/webauthn.rs).
const (
	flagUserPresent  = 0x01
	flagUserVerified = 0x04
)

func uvAuthData(uv bool) []byte {
	ad := make([]byte, 37)
	flags := byte(flagUserPresent)
	if uv {
		flags |= flagUserVerified
	}
	ad[32] = flags
	return ad
}

func uvEnvelope(uv bool) []byte {
	return phiante.WebAuthnSignature{
		AuthenticatorData: uvAuthData(uv),
		ClientDataJSON:    []byte(`{"type":"webauthn.get","challenge":"x","origin":"https://portphi.com"}`),
		Signature:         bytes.Repeat([]byte{0x07}, 64),
	}.Marshal()
}

func uvAwareVerifier() phicrypto.Verifier {
	return phicrypto.Fake{WebAuthnFn: func(a phicrypto.WebAuthnAssertion) bool {
		if len(a.AuthenticatorData) < 37 {
			return false
		}
		flags := a.AuthenticatorData[32]
		if flags&flagUserPresent == 0 {
			return false
		}
		if a.RequireUserVerification && flags&flagUserVerified == 0 {
			return false
		}
		return true
	}}
}

func uvWebAuthnParams() fakeWebAuthnParams {
	return fakeWebAuthnParams{origins: []string{"https://portphi.com"}, rpID: "portphi.com"}
}

func sensitiveUVPolicy() fakeUVPolicy {
	return fakeUVPolicy{
		sensitive: []string{
			sdk.MsgTypeURL(&identitytypes.MsgSetGuardians{}),
			sdk.MsgTypeURL(&identitytypes.MsgRotateIdentityKey{}),
		},
		largeTransfer: math.NewInt(100_000_000),
	}
}

// A passkey signer on a SENSITIVE message is rejected without the User-Verification flag and accepted with it; a NON-sensitive message needs only User-Presence.
func TestRouter_SteppedUV_SensitiveRequiresUV(t *testing.T) {
	sensitiveMsg := func(acc sdk.AccountI) sdk.Msg {
		return &identitytypes.MsgSetGuardians{
			Controller:  acc.GetAddress().String(),
			Did:         "did:phi:aa",
			Commitments: [][]byte{make([]byte, 32)},
			Threshold:   1,
		}
	}

	cases := []struct {
		name      string
		msg       func(sdk.AccountI) sdk.Msg
		uv        bool
		wantError bool
	}{
		{"sensitive without UV → rejected", sensitiveMsg, false, true},
		{"sensitive with UV → accepted", sensitiveMsg, true, false},
		{"non-sensitive without UV → accepted (UP only)", anteMsg, false, false},
		{"non-sensitive with UV → accepted", anteMsg, true, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAnteFixture(t)
			r1, err := secp256r1.GenPrivKey()
			require.NoError(t, err)
			acc := f.mkAccount(t, r1.PubKey(), 0)

			tx, _ := f.envelopeTxMsgs(t, r1.PubKey(), acc, uvEnvelope(tc.uv),
				signing.SignMode_SIGN_MODE_DIRECT, tc.msg(acc))

			dec := f.phiDecoratorUV(uvAwareVerifier(), uvWebAuthnParams(), sensitiveUVPolicy())
			_, err = f.run(dec, tx)
			if tc.wantError {
				require.Error(t, err, "a sensitive message signed without a User-Verification gesture must be rejected")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// The sensitive set is GOVERNED: with an empty policy the very same sensitive message needs only User-Presence, and once governance lists it the same UP-only assertion is rejected.
func TestRouter_SteppedUV_PolicyIsGoverned(t *testing.T) {
	build := func(t *testing.T) (anteFixture, sdk.Tx) {
		t.Helper()
		f := newAnteFixture(t)
		r1, err := secp256r1.GenPrivKey()
		require.NoError(t, err)
		acc := f.mkAccount(t, r1.PubKey(), 0)
		msg := &identitytypes.MsgSetGuardians{
			Controller:  acc.GetAddress().String(),
			Did:         "did:phi:aa",
			Commitments: [][]byte{make([]byte, 32)},
			Threshold:   1,
		}
		tx, _ := f.envelopeTxMsgs(t, r1.PubKey(), acc, uvEnvelope(false),
			signing.SignMode_SIGN_MODE_DIRECT, msg)
		return f, tx
	}

	f, tx := build(t)
	_, err := f.run(f.phiDecoratorUV(uvAwareVerifier(), uvWebAuthnParams(), noUVPolicy()), tx)
	require.NoError(t, err, "with an empty governed policy no message is sensitive")

	f2, tx2 := build(t)
	_, err = f2.run(f2.phiDecoratorUV(uvAwareVerifier(), uvWebAuthnParams(), sensitiveUVPolicy()), tx2)
	require.Error(t, err, "once governance marks the message sensitive, UP-only must be rejected")
}

// The amount rule: a transfer at or above the governed threshold is sensitive (UV required); a smaller one is not.
func TestRouter_SteppedUV_LargeTransfer(t *testing.T) {
	transfer := func(acc sdk.AccountI, amount string) sdk.Msg {
		return &cointypes.MsgTransfer{
			From:   acc.GetAddress().String(),
			To:     acc.GetAddress().String(),
			Amount: amount,
		}
	}

	cases := []struct {
		name      string
		amount    string
		uv        bool
		wantError bool
	}{
		{"large transfer without UV → rejected", "100000000", false, true},
		{"large transfer with UV → accepted", "100000000", true, false},
		{"small transfer without UV → accepted", "99999999", false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAnteFixture(t)
			r1, err := secp256r1.GenPrivKey()
			require.NoError(t, err)
			acc := f.mkAccount(t, r1.PubKey(), 0)

			tx, _ := f.envelopeTxMsgs(t, r1.PubKey(), acc, uvEnvelope(tc.uv),
				signing.SignMode_SIGN_MODE_DIRECT, transfer(acc, tc.amount))

			dec := f.phiDecoratorUV(uvAwareVerifier(), uvWebAuthnParams(), sensitiveUVPolicy())
			_, err = f.run(dec, tx)
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// A raw r1 signer on a NON-sensitive tx is unaffected by the stepped-UV policy: with a policy active but no sensitive message in the tx, the raw signature still verifies on the upstream path.
func TestRouter_SteppedUV_StandardSignersUnaffected(t *testing.T) {
	f := newAnteFixture(t)
	r1, err := secp256r1.GenPrivKey()
	require.NoError(t, err)
	acc := f.mkAccount(t, r1.PubKey(), 0)

	tx := f.signedTx(t, r1, acc) // a real r1 signature over a non-sensitive MsgSend, not a WebAuthn envelope
	dec := f.phiDecoratorUV(uvAwareVerifier(), uvWebAuthnParams(), sensitiveUVPolicy())
	_, err = f.run(dec, tx)
	require.NoError(t, err, "a raw r1 signer on a non-sensitive tx is unaffected by the stepped-UV policy")
}
