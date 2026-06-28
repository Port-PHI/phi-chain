// SPDX-License-Identifier: Apache-2.0

package phicrypto_test

import (
	"testing"

	"github.com/Port-PHI/phi-chain/phicrypto"
)

// Fake must be programmable and cover the happy/failure paths (interface-first core).

func TestFakeAcceptAll(t *testing.T) {
	v := phicrypto.AcceptAll()
	if !v.VerifySignature(phicrypto.Secp256r1, []byte{1}, []byte{2}, []byte{3}) {
		t.Fatal("AcceptAll must accept the signature")
	}
	if !v.VerifyWebAuthn(phicrypto.WebAuthnAssertion{}) {
		t.Fatal("AcceptAll must accept WebAuthn")
	}
	if !v.VerifyBBSProof([]byte{1}, []byte{2}, []byte{3}) {
		t.Fatal("AcceptAll must accept the BBS+ proof")
	}
}

func TestFakeRejectAllAndZeroValue(t *testing.T) {
	// Both RejectAll and the zero value of Fake must be fail-safe.
	for name, v := range map[string]phicrypto.Verifier{
		"RejectAll": phicrypto.RejectAll(),
		"ZeroValue": phicrypto.Fake{},
	} {
		if v.VerifySignature(phicrypto.Secp256k1, nil, nil, nil) ||
			v.VerifyWebAuthn(phicrypto.WebAuthnAssertion{}) ||
			v.VerifyBBSProof(nil, nil, nil) {
			t.Fatalf("%s must reject everything (fail-safe)", name)
		}
	}
}

func TestFakeCustomFnReceivesArgs(t *testing.T) {
	var gotCurve phicrypto.Curve
	v := phicrypto.Fake{
		SignatureFn: func(curve phicrypto.Curve, _, _, _ []byte) bool {
			gotCurve = curve
			return curve == phicrypto.Secp256r1
		},
	}
	if !v.VerifySignature(phicrypto.Secp256r1, nil, nil, nil) {
		t.Fatal("the injected function must return true for r1")
	}
	if gotCurve != phicrypto.Secp256r1 {
		t.Fatalf("wrong curve received: %d", gotCurve)
	}
	if v.VerifySignature(phicrypto.Secp256k1, nil, nil, nil) {
		t.Fatal("the injected function must return false for k1")
	}
}
