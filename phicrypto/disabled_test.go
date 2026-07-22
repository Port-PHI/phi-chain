// SPDX-License-Identifier: Apache-2.0

//go:build !phicrypto_cgo

package phicrypto_test

import (
	"testing"

	"github.com/Port-PHI/phi-chain/phicrypto"
)

// Without the phicrypto_cgo tag, Default must be [phicrypto.Disabled] and reject everything (fail-safe).
func TestDefaultIsDisabledAndRejects(t *testing.T) {
	v := phicrypto.Default()
	if _, ok := v.(phicrypto.Disabled); !ok {
		t.Fatalf("without the cgo tag, Default must be Disabled, not %T", v)
	}
	if v.VerifySignature(phicrypto.Secp256r1, []byte{1}, []byte{2}, []byte{3}) {
		t.Fatal("Disabled must reject the signature")
	}
	if v.VerifyWebAuthn(phicrypto.WebAuthnAssertion{PublicKey: []byte{1}}) {
		t.Fatal("Disabled must reject WebAuthn")
	}
	if v.VerifyBBSProof([]byte{1}, []byte{2}, []byte{3}) {
		t.Fatal("Disabled must reject the BBS+ proof")
	}
}
