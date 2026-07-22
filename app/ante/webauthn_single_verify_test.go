// SPDX-License-Identifier: Apache-2.0

package ante_test

import (
	"bytes"
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	phiante "github.com/Port-PHI/phi-chain/app/ante"
	"github.com/Port-PHI/phi-chain/phicrypto"
	identitytypes "github.com/Port-PHI/phi-chain/x/identity/types"
)

func countingVerifier(calls *int) phicrypto.Fake {
	inner := originBindingVerifier()
	return phicrypto.Fake{WebAuthnFn: func(a phicrypto.WebAuthnAssertion) bool {
		*calls++
		return inner.WebAuthnFn(a)
	}}
}

func manyOrigins(target string) []string {
	origins := make([]string, 0, identitytypes.MaxWebAuthnAllowedOrigins)
	for i := 0; i < identitytypes.MaxWebAuthnAllowedOrigins-1; i++ {
		origins = append(origins, fmt.Sprintf("https://decoy-%02d.example", i))
	}
	return append(origins, target)
}

// A valid assertion costs exactly one verification even at the end of a full-size allow-list.
func TestWebAuthn_ValidAssertionCostsOneVerification(t *testing.T) {
	ctx := sdk.Context{}
	pub := bytes.Repeat([]byte{0x02}, 33)
	signBytes := []byte("sb")
	target := "https://app.portphi.com"

	calls := 0
	d := phiante.NewWebAuthnDecorator(countingVerifier(&calls), fakeWebAuthnParams{
		origins: manyOrigins(target), rpID: "portphi.com",
	}, noUVPolicy())

	handled, err := d.VerifyEnvelope(ctx, pub, signBytes, envelopeForOrigin(target).Marshal(), false)
	require.True(t, handled)
	require.NoError(t, err, "an assertion under an allowed origin must be accepted")
	require.Equal(t, 1, calls, "a valid assertion must cost exactly one signature verification")
}

// A rejected assertion must cost no verification (a CheckTx failure pays no fee).
func TestWebAuthn_RejectedOriginCostsNoVerification(t *testing.T) {
	ctx := sdk.Context{}
	pub := bytes.Repeat([]byte{0x02}, 33)
	signBytes := []byte("sb")

	calls := 0
	d := phiante.NewWebAuthnDecorator(countingVerifier(&calls), fakeWebAuthnParams{
		origins: manyOrigins("https://app.portphi.com"), rpID: "portphi.com",
	}, noUVPolicy())

	handled, err := d.VerifyEnvelope(ctx, pub, signBytes, envelopeForOrigin("https://phish.example").Marshal(), false)
	require.True(t, handled)
	require.Error(t, err, "an assertion whose origin is not allowed must be rejected")
	require.Equal(t, 0, calls,
		"an assertion from a disallowed origin must be rejected without any signature verification")
}

// The verification work must not scale with the allow-list size.
func TestWebAuthn_VerificationCountIsIndependentOfAllowListSize(t *testing.T) {
	ctx := sdk.Context{}
	pub := bytes.Repeat([]byte{0x02}, 33)
	signBytes := []byte("sb")
	target := "https://app.portphi.com"

	measure := func(origins []string) int {
		calls := 0
		d := phiante.NewWebAuthnDecorator(countingVerifier(&calls), fakeWebAuthnParams{
			origins: origins, rpID: "portphi.com",
		}, noUVPolicy())
		_, err := d.VerifyEnvelope(ctx, pub, signBytes, envelopeForOrigin(target).Marshal(), false)
		require.NoError(t, err)
		return calls
	}

	require.Equal(t, measure([]string{target}), measure(manyOrigins(target)),
		"verification work must not grow with the number of allowed origins")
}

// Unparseable client data is rejected before any verification is attempted.
func TestWebAuthn_UnparseableClientDataCostsNoVerification(t *testing.T) {
	ctx := sdk.Context{}
	pub := bytes.Repeat([]byte{0x02}, 33)
	signBytes := []byte("sb")

	for _, tc := range []struct {
		name       string
		clientData string
	}{
		{"not json", `this-is-not-json`},
		{"empty object", `{}`},
		{"origin is empty", `{"type":"webauthn.get","origin":""}`},
		{"truncated json", `{"origin":`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := sampleEnvelope()
			env.ClientDataJSON = []byte(tc.clientData)

			calls := 0
			d := phiante.NewWebAuthnDecorator(countingVerifier(&calls), fakeWebAuthnParams{
				origins: manyOrigins("https://app.portphi.com"), rpID: "portphi.com",
			}, noUVPolicy())

			handled, err := d.VerifyEnvelope(ctx, pub, signBytes, env.Marshal(), false)
			require.True(t, handled)
			require.Error(t, err, "unusable client data must be rejected")
			require.Equal(t, 0, calls, "unusable client data must cost no verification")
		})
	}
}

// An assertion is accepted iff its signed origin is on the governed list.
func TestWebAuthn_OriginDecisionIsUnchanged(t *testing.T) {
	ctx := sdk.Context{}
	pub := bytes.Repeat([]byte{0x02}, 33)
	signBytes := []byte("sb")
	allowed := []string{"https://portphi.com", "https://app.portphi.com"}

	for _, tc := range []struct {
		origin string
		accept bool
	}{
		{"https://portphi.com", true},
		{"https://app.portphi.com", true},
		{"https://phish.example", false},
		{"https://portphi.com.evil.example", false},
		{"http://portphi.com", false},
		{"https://portphi.com/", false},
	} {
		t.Run(tc.origin, func(t *testing.T) {
			calls := 0
			d := phiante.NewWebAuthnDecorator(countingVerifier(&calls), fakeWebAuthnParams{
				origins: allowed, rpID: "portphi.com",
			}, noUVPolicy())

			handled, err := d.VerifyEnvelope(ctx, pub, signBytes, envelopeForOrigin(tc.origin).Marshal(), false)
			require.True(t, handled)
			if tc.accept {
				require.NoError(t, err)
				require.Equal(t, 1, calls)
			} else {
				require.Error(t, err)
				require.Equal(t, 0, calls)
			}
		})
	}
}
