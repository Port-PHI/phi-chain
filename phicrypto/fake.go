// SPDX-License-Identifier: Apache-2.0

package phicrypto

// Fake is a programmable Verifier for TESTS (not production). Any unset (nil)
// function behaves fail-safe (false); so `Fake{}` rejects everything. The
// credentials/disclosure/voting modules and the ante inject one of these in their
// tests to verify chain logic without needing phi-crypto/cgo (the heart of the
// interface-first approach).
type Fake struct {
	SignatureFn     func(curve Curve, publicKey, msg, sig []byte) bool
	WebAuthnFn      func(a WebAuthnAssertion) bool
	BBSProofFn      func(proof, issuerPublicKey, nonce []byte) bool
	SemaphoreVoteFn func(proof, issuerPublicKey, electionID, nullifier, signal []byte) bool
}

// VerifySignature delegates to SignatureFn (nil → false).
func (f Fake) VerifySignature(curve Curve, publicKey, msg, sig []byte) bool {
	if f.SignatureFn == nil {
		return false
	}
	return f.SignatureFn(curve, publicKey, msg, sig)
}

// VerifyWebAuthn delegates to WebAuthnFn (nil → false).
func (f Fake) VerifyWebAuthn(a WebAuthnAssertion) bool {
	if f.WebAuthnFn == nil {
		return false
	}
	return f.WebAuthnFn(a)
}

// VerifyBBSProof delegates to BBSProofFn (nil → false).
func (f Fake) VerifyBBSProof(proof, issuerPublicKey, nonce []byte) bool {
	if f.BBSProofFn == nil {
		return false
	}
	return f.BBSProofFn(proof, issuerPublicKey, nonce)
}

// VerifySemaphoreVote delegates to SemaphoreVoteFn (nil → false).
func (f Fake) VerifySemaphoreVote(proof, issuerPublicKey, electionID, nullifier, signal []byte) bool {
	if f.SemaphoreVoteFn == nil {
		return false
	}
	return f.SemaphoreVoteFn(proof, issuerPublicKey, electionID, nullifier, signal)
}

// AcceptAll builds a Fake that accepts every verification (test happy path).
func AcceptAll() Fake {
	return Fake{
		SignatureFn:     func(Curve, []byte, []byte, []byte) bool { return true },
		WebAuthnFn:      func(WebAuthnAssertion) bool { return true },
		BBSProofFn:      func([]byte, []byte, []byte) bool { return true },
		SemaphoreVoteFn: func([]byte, []byte, []byte, []byte, []byte) bool { return true },
	}
}

// RejectAll builds a Fake that rejects everything (test failure path).
func RejectAll() Fake { return Fake{} }
