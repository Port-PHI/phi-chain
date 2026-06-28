// SPDX-License-Identifier: Apache-2.0

//go:build phicrypto_cgo

package phicrypto_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/phicrypto"
)

// TestCGO_RealVerifierLinksAndFailsSafe runs only under the `phicrypto_cgo` build tag, i.e. when the
// chain is linked against the real phi-crypto C-ABI (libphi_crypto). It proves the bridge links and
// is callable across the FFI boundary without crashing, that Default() is the real CGO verifier
// (phicrypto.Disabled does not exist in this build), and that malformed inputs fail closed. Positive
// cryptographic correctness (valid signatures/proofs) is covered by phi-crypto's own KAT/Rust tests.
func TestCGO_RealVerifierLinksAndFailsSafe(t *testing.T) {
	v := phicrypto.Default() // CGO{} under the phicrypto_cgo tag

	// Signature verification: garbage key/msg/sig must fail closed on both curves.
	require.False(t, v.VerifySignature(phicrypto.Secp256r1, []byte("not-a-key"), []byte("msg"), []byte("sig")))
	require.False(t, v.VerifySignature(phicrypto.Secp256k1, nil, nil, nil))

	// WebAuthn assertion with malformed fields must fail closed.
	require.False(t, v.VerifyWebAuthn(phicrypto.WebAuthnAssertion{
		AuthenticatorData: []byte("ad"),
		ClientDataJSON:    []byte("{}"),
		Signature:         []byte("s"),
		Challenge:         []byte("c"),
		PublicKey:         []byte("pk"),
		Origin:            "https://portphi.com",
		RPID:              "portphi.com",
	}))

	// BBS+ proof verification: garbage proof/key/nonce must fail closed.
	require.False(t, v.VerifyBBSProof([]byte("proof"), []byte("issuer"), []byte("nonce")))
}
