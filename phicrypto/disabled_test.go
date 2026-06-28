// SPDX-License-Identifier: Apache-2.0

//go:build !phicrypto_cgo

package phicrypto_test

import (
	"testing"

	"github.com/Port-PHI/phi-chain/phicrypto"
)

// In a build without the phicrypto_cgo tag, the default must be
// [phicrypto.Disabled] and must reject everything (fail-safe). This guarantees a
// node built without the phi-crypto link does not accept a crypto-dependent
// transaction.
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
