// SPDX-License-Identifier: Apache-2.0

package ante_test

import (
	"bytes"
	"crypto/sha256"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	phiante "github.com/Port-PHI/phi-chain/app/ante"
	"github.com/Port-PHI/phi-chain/phicrypto"
)

func sampleEnvelope() phiante.WebAuthnSignature {
	return phiante.WebAuthnSignature{
		AuthenticatorData: []byte("authenticator-data"),
		ClientDataJSON:    []byte(`{"type":"webauthn.get","challenge":"abc","origin":"https://portphi.com"}`),
		Signature:         bytes.Repeat([]byte{0x07}, 64),
	}
}

// fakeWebAuthnParams is a test WebAuthnParamSource with a fixed governed relying-party config.
type fakeWebAuthnParams struct {
	origins []string
	rpID    string
}

func (f fakeWebAuthnParams) WebAuthnRelyingParty(_ sdk.Context) ([]string, string) {
	return f.origins, f.rpID
}

func TestWebAuthnSignature_RoundTrip(t *testing.T) {
	in := sampleEnvelope()
	out, err := phiante.UnmarshalWebAuthnSignature(in.Marshal())
	require.NoError(t, err)
	require.Equal(t, in.AuthenticatorData, out.AuthenticatorData)
	require.Equal(t, in.ClientDataJSON, out.ClientDataJSON)
	require.Equal(t, in.Signature, out.Signature)
}

func TestIsWebAuthnEnvelope(t *testing.T) {
	require.True(t, phiante.IsWebAuthnEnvelope(sampleEnvelope().Marshal()))
	// A raw 64-byte secp256r1 signature is not an envelope.
	require.False(t, phiante.IsWebAuthnEnvelope(bytes.Repeat([]byte{0x01}, 64)))
	// A DER signature (leading 0x30) is not an envelope.
	require.False(t, phiante.IsWebAuthnEnvelope([]byte{0x30, 0x44, 0x02, 0x20}))
	require.False(t, phiante.IsWebAuthnEnvelope([]byte{0x50})) // too short
}

func TestUnmarshalWebAuthnSignature_Rejects(t *testing.T) {
	// missing magic
	_, err := phiante.UnmarshalWebAuthnSignature([]byte("XXXX\x00\x00\x00\x00"))
	require.Error(t, err)
	// truncated (magic only, no length prefix)
	_, err = phiante.UnmarshalWebAuthnSignature([]byte("PWA1"))
	require.Error(t, err)
	// trailing bytes after a valid envelope
	bad := append(sampleEnvelope().Marshal(), 0xFF)
	_, err = phiante.UnmarshalWebAuthnSignature(bad)
	require.Error(t, err)
}

func TestWebAuthnChallenge(t *testing.T) {
	sb := []byte("sign-bytes")
	c := phiante.WebAuthnChallenge(sb)
	require.Len(t, c, 32)
	// Domain-separated: SHA256("PHI-WEBAUTHN-v1" ‖ signBytes), not the bare SHA256(signBytes).
	want := sha256.Sum256(append([]byte("PHI-WEBAUTHN-v1"), sb...))
	require.Equal(t, want[:], c)
	plain := sha256.Sum256(sb)
	require.NotEqual(t, plain[:], c, "challenge must be domain-separated, not bare SHA256(signBytes)")
	// different sign-bytes → different challenge
	require.NotEqual(t, c, phiante.WebAuthnChallenge([]byte("other")))
}

func TestVerifyWebAuthnAssertion_AcceptAndReject(t *testing.T) {
	env := sampleEnvelope()
	pub := bytes.Repeat([]byte{0x02}, 33)
	signBytes := []byte("the-sign-bytes")

	require.NoError(t, phiante.VerifyWebAuthnAssertion(phicrypto.AcceptAll(), env, pub, signBytes, "https://portphi.com", "portphi.com"))
	require.Error(t, phiante.VerifyWebAuthnAssertion(phicrypto.RejectAll(), env, pub, signBytes, "https://portphi.com", "portphi.com"))
}

// TestVerifyWebAuthnAssertion_BuildsBoundAssertion confirms the ante passes the
// correct assertion to the port: challenge = WebAuthnChallenge(signBytes) (domain-separated),
// the envelope fields, the pubkey, and the configured origin/rpId.
func TestVerifyWebAuthnAssertion_BuildsBoundAssertion(t *testing.T) {
	env := sampleEnvelope()
	pub := bytes.Repeat([]byte{0x02}, 33)
	signBytes := []byte("the-sign-bytes")

	var got phicrypto.WebAuthnAssertion
	capture := phicrypto.Fake{WebAuthnFn: func(a phicrypto.WebAuthnAssertion) bool {
		got = a
		return true
	}}

	require.NoError(t, phiante.VerifyWebAuthnAssertion(capture, env, pub, signBytes, "https://portphi.com", "portphi.com"))

	want := sha256.Sum256(append([]byte("PHI-WEBAUTHN-v1"), signBytes...))
	require.Equal(t, want[:], got.Challenge)
	require.Equal(t, env.AuthenticatorData, got.AuthenticatorData)
	require.Equal(t, env.ClientDataJSON, got.ClientDataJSON)
	require.Equal(t, env.Signature, got.Signature)
	require.Equal(t, pub, got.PublicKey)
	require.Equal(t, "https://portphi.com", got.Origin)
	require.Equal(t, "portphi.com", got.RPID)
}

// TestVerifyWebAuthnAssertion_DefaultIsFailSafe confirms that without the cgo
// build (Disabled verifier) every assertion is rejected.
func TestVerifyWebAuthnAssertion_DefaultIsFailSafe(t *testing.T) {
	env := sampleEnvelope()
	err := phiante.VerifyWebAuthnAssertion(phicrypto.Default(), env, bytes.Repeat([]byte{0x02}, 33), []byte("sb"), "https://portphi.com", "portphi.com")
	require.Error(t, err)
}

// TestWebAuthnDecorator_VerifyEnvelope exercises the verifier-backed routing decision: non-envelope
// signatures are left to the standard path; envelopes are verified via the port (fail-closed).
func TestWebAuthnDecorator_VerifyEnvelope(t *testing.T) {
	pub := bytes.Repeat([]byte{0x02}, 33)
	signBytes := []byte("the-sign-bytes")
	envBytes := sampleEnvelope().Marshal()
	rawSig := bytes.Repeat([]byte{0x01}, 64) // a plain secp256r1 signature, not an envelope

	ctx := sdk.Context{}
	params := fakeWebAuthnParams{origins: []string{"https://portphi.com"}, rpID: "portphi.com"}

	// A non-envelope signature is not handled here (left to the standard signature path).
	d := phiante.NewWebAuthnDecorator(phicrypto.AcceptAll(), params)
	handled, err := d.VerifyEnvelope(ctx, pub, signBytes, rawSig)
	require.False(t, handled)
	require.NoError(t, err)

	// An envelope is handled and verifies when the port accepts (under an allowed origin).
	handled, err = d.VerifyEnvelope(ctx, pub, signBytes, envBytes)
	require.True(t, handled)
	require.NoError(t, err)

	// An envelope is handled and rejected (fail-closed) when the port rejects.
	dReject := phiante.NewWebAuthnDecorator(phicrypto.RejectAll(), params)
	handled, err = dReject.VerifyEnvelope(ctx, pub, signBytes, envBytes)
	require.True(t, handled)
	require.Error(t, err)

	// A nil verifier falls back to the fail-safe Disabled port: every envelope is rejected.
	dDefault := phiante.NewWebAuthnDecorator(nil, params)
	handled, err = dDefault.VerifyEnvelope(ctx, pub, signBytes, envBytes)
	require.True(t, handled)
	require.Error(t, err)

	// A malformed envelope (magic but truncated) is handled and rejected.
	handled, err = d.VerifyEnvelope(ctx, pub, signBytes, []byte("PWA1\x00\x00"))
	require.True(t, handled)
	require.Error(t, err)
}

// TestWebAuthnDecorator_OriginAllowList proves the governed origin allow-list: an
// assertion is accepted only if it verifies under one of the configured origins, and the set may hold
// multiple origins (web + native app).
func TestWebAuthnDecorator_OriginAllowList(t *testing.T) {
	ctx := sdk.Context{}
	pub := bytes.Repeat([]byte{0x02}, 33)
	signBytes := []byte("sb")
	envBytes := sampleEnvelope().Marshal()

	// A verifier that accepts ONLY the native-app origin (models phi-crypto's origin check).
	appOnly := phicrypto.Fake{WebAuthnFn: func(a phicrypto.WebAuthnAssertion) bool {
		return a.Origin == "https://app.portphi.com"
	}}

	// The allow-list contains the app origin → accepted (tried after the web origin fails).
	dMulti := phiante.NewWebAuthnDecorator(appOnly, fakeWebAuthnParams{
		origins: []string{"https://portphi.com", "https://app.portphi.com"}, rpID: "portphi.com",
	})
	handled, err := dMulti.VerifyEnvelope(ctx, pub, signBytes, envBytes)
	require.True(t, handled)
	require.NoError(t, err, "an assertion under an allowed origin must be accepted")

	// The allow-list excludes the app origin → rejected (anti-phishing).
	dNarrow := phiante.NewWebAuthnDecorator(appOnly, fakeWebAuthnParams{
		origins: []string{"https://portphi.com"}, rpID: "portphi.com",
	})
	handled, err = dNarrow.VerifyEnvelope(ctx, pub, signBytes, envBytes)
	require.True(t, handled)
	require.Error(t, err, "an assertion whose origin is not in the allow-list must be rejected")
}
