// SPDX-License-Identifier: Apache-2.0

//go:build phicrypto_cgo

package phicrypto_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/phicrypto"
)

// TestCGO_RealVerifierLinksAndFailsSafe (phicrypto_cgo tag): proves the C-ABI bridge links, Default() is the real CGO verifier, and malformed inputs fail closed.
func TestCGO_RealVerifierLinksAndFailsSafe(t *testing.T) {
	v := phicrypto.Default()

	require.False(t, v.VerifySignature(phicrypto.Secp256r1, []byte("not-a-key"), []byte("msg"), []byte("sig")))
	require.False(t, v.VerifySignature(phicrypto.Secp256k1, nil, nil, nil))

	require.False(t, v.VerifyWebAuthn(phicrypto.WebAuthnAssertion{
		AuthenticatorData: []byte("ad"),
		ClientDataJSON:    []byte("{}"),
		Signature:         []byte("s"),
		Challenge:         []byte("c"),
		PublicKey:         []byte("pk"),
		Origin:            "https://portphi.com",
		RPID:              "portphi.com",
	}))

	require.False(t, v.VerifyBBSProof([]byte("proof"), []byte("issuer"), []byte("nonce")))
}
