// SPDX-License-Identifier: Apache-2.0

//go:build phicrypto_cgo

package phicrypto_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/phicrypto"
)

// WebAuthn authenticatorData flag bits (mirrors phi-crypto src/webauthn.rs).
const (
	flagUserPresent  = 0x01
	flagUserVerified = 0x04
)

func buildAssertion(t *testing.T, uv bool, challenge []byte, origin, rpID string) phicrypto.WebAuthnAssertion {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	clientData := []byte(fmt.Sprintf(
		`{"type":"webauthn.get","challenge":"%s","origin":"%s","crossOrigin":false}`,
		base64.RawURLEncoding.EncodeToString(challenge), origin))

	rpIDHash := sha256.Sum256([]byte(rpID))
	flags := byte(flagUserPresent)
	if uv {
		flags |= flagUserVerified
	}
	authData := append(append([]byte{}, rpIDHash[:]...), flags)
	authData = append(authData, 0, 0, 0, 1)

	clientDataHash := sha256.Sum256(clientData)
	signed := append(append([]byte{}, authData...), clientDataHash[:]...)
	digest := sha256.Sum256(signed)

	r, s, err := ecdsa.Sign(rand.Reader, priv, digest[:])
	require.NoError(t, err)
	halfN := new(big.Int).Rsh(elliptic.P256().Params().N, 1)
	if s.Cmp(halfN) > 0 {
		s = new(big.Int).Sub(elliptic.P256().Params().N, s)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])

	return phicrypto.WebAuthnAssertion{
		AuthenticatorData: authData,
		ClientDataJSON:    clientData,
		Signature:         sig,
		Challenge:         challenge,
		PublicKey:         elliptic.Marshal(elliptic.P256(), priv.PublicKey.X, priv.PublicKey.Y),
		Origin:            origin,
		RPID:              rpID,
	}
}

// Stepped User-Verification across the real C-ABI: a UV assertion satisfies both policies; a UP-only assertion passes require_uv=false and is rejected under require_uv=true.
func TestCGO_WebAuthn_RequireUserVerification(t *testing.T) {
	v := phicrypto.Default()
	const origin, rpID = "https://portphi.com", "portphi.com"
	challenge := []byte("phi-tx-sign-doc-hash-0001")

	withUV := buildAssertion(t, true, challenge, origin, rpID)
	upOnly := buildAssertion(t, false, challenge, origin, rpID)

	withUV.RequireUserVerification = true
	require.True(t, v.VerifyWebAuthn(withUV), "UV assertion must pass require_uv=true")
	withUV.RequireUserVerification = false
	require.True(t, v.VerifyWebAuthn(withUV), "UV assertion must pass require_uv=false")

	upOnly.RequireUserVerification = false
	require.True(t, v.VerifyWebAuthn(upOnly), "UP-only assertion must pass require_uv=false")
	upOnly.RequireUserVerification = true
	require.False(t, v.VerifyWebAuthn(upOnly), "UP-only assertion must be REJECTED when UV is required")
}
