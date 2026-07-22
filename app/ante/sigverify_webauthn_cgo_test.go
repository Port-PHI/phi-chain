//go:build phicrypto_cgo

// SPDX-License-Identifier: Apache-2.0

package ante_test

import (
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math/big"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256r1"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/stretchr/testify/require"

	phiante "github.com/Port-PHI/phi-chain/app/ante"
	"github.com/Port-PHI/phi-chain/phicrypto"
)

func buildRealAssertion(t *testing.T, priv *secp256r1.PrivKey, challenge []byte, origin, rpID string, up, highS bool) []byte {
	t.Helper()
	clientDataJSON := []byte(fmt.Sprintf(
		`{"type":"webauthn.get","challenge":"%s","origin":"%s","crossOrigin":false}`,
		base64.RawURLEncoding.EncodeToString(challenge), origin,
	))

	rpHash := sha256.Sum256([]byte(rpID))
	flags := byte(0x00)
	if up {
		flags = 0x01 // User-Presence
	}
	authData := append([]byte{}, rpHash[:]...)
	authData = append(authData, flags)
	authData = append(authData, 0, 0, 0, 1)

	cdjHash := sha256.Sum256(clientDataJSON)
	signed := append([]byte{}, authData...)
	signed = append(signed, cdjHash[:]...)

	sig, err := priv.Sign(signed)
	require.NoError(t, err)
	if highS {
		sig = toHighS(sig)
	}

	return phiante.WebAuthnSignature{
		AuthenticatorData: authData,
		ClientDataJSON:    clientDataJSON,
		Signature:         sig,
	}.Marshal()
}

func toHighS(sig []byte) []byte {
	n := elliptic.P256().Params().N
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:64])
	s.Sub(n, s)
	out := make([]byte, 64)
	r.FillBytes(out[:32])
	s.FillBytes(out[32:64])
	return out
}

const (
	cgoOrigin = "https://portphi.com"
	cgoRPID   = "portphi.com"
)

// A genuine passkey assertion is accepted by the live verifier; the router forwards compressed SEC1 (33B).
func TestPhiSigVerify_CGO_ValidAssertionAccepted(t *testing.T) {
	require.True(t, phicrypto.DefaultEnforces(), "this test requires the real phi-crypto verifier (-tags phicrypto_cgo)")
	f := newAnteFixture(t)
	priv, err := secp256r1.GenPrivKey()
	require.NoError(t, err)
	acc := f.mkAccount(t, priv.PubKey(), 0)
	params := fakeWebAuthnParams{origins: []string{cgoOrigin}, rpID: cgoRPID}

	_, signBytes := f.envelopeTx(t, priv.PubKey(), acc, sampleEnvelope().Marshal(), signing.SignMode_SIGN_MODE_DIRECT)
	challenge := phiante.WebAuthnChallenge(signBytes)

	env := buildRealAssertion(t, priv, challenge, cgoOrigin, cgoRPID, true, false)
	tx, signBytes2 := f.envelopeTx(t, priv.PubKey(), acc, env, signing.SignMode_SIGN_MODE_DIRECT)
	require.Equal(t, signBytes, signBytes2, "sign-bytes must be independent of the signature field")

	_, err = f.run(f.phiDecorator(phicrypto.Default(), params), tx)
	require.NoError(t, err, "the live phi-crypto verifier must accept a valid passkey assertion")

	require.Len(t, priv.PubKey().Bytes(), 33, "guardrail 6: the router forwards compressed SEC1 P-256 (33B), which phi-crypto accepts")
}

// Each check inside phi-crypto rejects through the router, fail-closed: missing UP, wrong origin, high-S sig.
func TestPhiSigVerify_CGO_RealChecksReject(t *testing.T) {
	require.True(t, phicrypto.DefaultEnforces(), "requires -tags phicrypto_cgo")
	params := fakeWebAuthnParams{origins: []string{cgoOrigin}, rpID: cgoRPID}

	cases := []struct {
		name         string
		assertOrigin string
		up           bool
		highS        bool
	}{
		{"missing user-presence", cgoOrigin, false, false},
		{"phishing origin", "https://phishing.example", true, false},
		{"high-S signature (malleable)", cgoOrigin, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAnteFixture(t)
			priv, err := secp256r1.GenPrivKey()
			require.NoError(t, err)
			acc := f.mkAccount(t, priv.PubKey(), 0)

			_, signBytes := f.envelopeTx(t, priv.PubKey(), acc, sampleEnvelope().Marshal(), signing.SignMode_SIGN_MODE_DIRECT)
			challenge := phiante.WebAuthnChallenge(signBytes)
			env := buildRealAssertion(t, priv, challenge, tc.assertOrigin, cgoRPID, tc.up, tc.highS)
			tx, _ := f.envelopeTx(t, priv.PubKey(), acc, env, signing.SignMode_SIGN_MODE_DIRECT)

			_, err = f.run(f.phiDecorator(phicrypto.Default(), params), tx)
			require.Error(t, err, "%s must be rejected by the live verifier", tc.name)
			require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
		})
	}
}

// Anti-replay: an assertion bound to tx A does not authenticate tx B under the real verifier.
func TestPhiSigVerify_CGO_AntiReplay(t *testing.T) {
	require.True(t, phicrypto.DefaultEnforces(), "requires -tags phicrypto_cgo")
	f := newAnteFixture(t)
	priv, err := secp256r1.GenPrivKey()
	require.NoError(t, err)
	params := fakeWebAuthnParams{origins: []string{cgoOrigin}, rpID: cgoRPID}
	dec := f.phiDecorator(phicrypto.Default(), params)

	accA := f.mkAccount(t, priv.PubKey(), 0)
	_, signBytesA := f.envelopeTx(t, priv.PubKey(), accA, sampleEnvelope().Marshal(), signing.SignMode_SIGN_MODE_DIRECT)
	env := buildRealAssertion(t, priv, phiante.WebAuthnChallenge(signBytesA), cgoOrigin, cgoRPID, true, false)

	txA, _ := f.envelopeTx(t, priv.PubKey(), accA, env, signing.SignMode_SIGN_MODE_DIRECT)
	_, err = f.run(dec, txA)
	require.NoError(t, err, "the assertion authenticates its own tx A under the live verifier")

	accB := f.mkAccount(t, priv.PubKey(), 1)
	txB, signBytesB := f.envelopeTx(t, priv.PubKey(), accB, env, signing.SignMode_SIGN_MODE_DIRECT)
	require.NotEqual(t, signBytesA, signBytesB)
	_, err = f.run(dec, txB)
	require.Error(t, err, "an assertion bound to tx A must not authenticate tx B (anti-replay, real verifier)")
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
}
