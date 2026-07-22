//go:build phicrypto_cgo

// SPDX-License-Identifier: Apache-2.0

package ante_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256r1"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/stretchr/testify/require"

	phiante "github.com/Port-PHI/phi-chain/app/ante"
	"github.com/Port-PHI/phi-chain/phicrypto"
)

func assertionFromCDJ(t *testing.T, priv *secp256r1.PrivKey, clientDataJSON []byte, rpID string) []byte {
	t.Helper()
	rpHash := sha256.Sum256([]byte(rpID))
	authData := append([]byte{}, rpHash[:]...)
	authData = append(authData, 0x01)       // User-Presence
	authData = append(authData, 0, 0, 0, 1) // signature counter
	cdjHash := sha256.Sum256(clientDataJSON)
	signed := append(append([]byte{}, authData...), cdjHash[:]...)
	sig, err := priv.Sign(signed)
	require.NoError(t, err)
	return phiante.WebAuthnSignature{
		AuthenticatorData: authData,
		ClientDataJSON:    clientDataJSON,
		Signature:         sig,
	}.Marshal()
}

func goParsedOrigin(t *testing.T, clientDataJSON []byte) string {
	t.Helper()
	var cd struct {
		Origin string `json:"origin"`
	}
	require.NoError(t, json.Unmarshal(clientDataJSON, &cd))
	require.NotEmpty(t, cd.Origin)
	return cd.Origin
}

func runOrigin(t *testing.T, clientDataJSON func(challenge []byte) []byte, allow []string) error {
	t.Helper()
	f := newAnteFixture(t)
	priv, err := secp256r1.GenPrivKey()
	require.NoError(t, err)
	acc := f.mkAccount(t, priv.PubKey(), 0)
	params := fakeWebAuthnParams{origins: allow, rpID: cgoRPID}

	_, signBytes := f.envelopeTx(t, priv.PubKey(), acc, sampleEnvelope().Marshal(), signing.SignMode_SIGN_MODE_DIRECT)
	challenge := phiante.WebAuthnChallenge(signBytes)

	env := assertionFromCDJ(t, priv, clientDataJSON(challenge), cgoRPID)
	tx, _ := f.envelopeTx(t, priv.PubKey(), acc, env, signing.SignMode_SIGN_MODE_DIRECT)
	_, err = f.run(f.phiDecorator(phicrypto.Default(), params), tx)
	return err
}

// A clientDataJSON carrying the origin key TWICE.
func TestWebAuthnOrigin_CGO_DuplicateKeyIsFailClosed(t *testing.T) {
	require.True(t, phicrypto.DefaultEnforces(), "requires the real phi-crypto verifier (-tags phicrypto_cgo)")

	const first = "https://shadowed.example"
	const last = cgoOrigin
	cdj := func(challenge []byte) []byte {
		return []byte(`{"type":"webauthn.get","challenge":"` +
			base64.RawURLEncoding.EncodeToString(challenge) +
			`","origin":"` + first + `","origin":"` + last + `"}`)
	}

	require.Equal(t, last, goParsedOrigin(t, cdj([]byte("x"))), "Go must resolve a duplicate origin to the last")

	for _, allow := range [][]string{{last}, {first}, {first, last}} {
		err := runOrigin(t, cdj, allow)
		require.Error(t, err, "a duplicate-origin clientDataJSON must be rejected (allow=%v)", allow)
		require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	}
}

// A non-canonical (but single-origin) clientDataJSON — extra whitespace and reordered keys — must still have its origin extracted identically by Go and Rust, so a genuine assertion is accepted.
func TestWebAuthnOrigin_CGO_NonCanonicalAgreesAcrossLanguages(t *testing.T) {
	require.True(t, phicrypto.DefaultEnforces(), "requires the real phi-crypto verifier (-tags phicrypto_cgo)")

	cdj := func(challenge []byte) []byte {
		return []byte("{  \"origin\" : \"" + cgoOrigin + "\" ,\n  \"type\":\"webauthn.get\", \"challenge\" : \"" +
			base64.RawURLEncoding.EncodeToString(challenge) + "\"  }")
	}
	require.Equal(t, cgoOrigin, goParsedOrigin(t, cdj([]byte("x"))))
	require.NoError(t, runOrigin(t, cdj, []string{cgoOrigin}),
		"a non-canonical but valid clientDataJSON must be accepted — Go and Rust agree on the origin")
}
