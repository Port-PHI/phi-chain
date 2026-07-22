// SPDX-License-Identifier: Apache-2.0

package phicrypto

// Fake is a programmable Verifier for TESTS; any unset (nil) function is fail-safe (false), so `Fake{}` rejects everything.
type Fake struct {
	SignatureFn      func(curve Curve, publicKey, msg, sig []byte) bool
	WebAuthnFn       func(a WebAuthnAssertion) bool
	BBSProofFn       func(proof, issuerPublicKey, nonce []byte) bool
	SemaphoreVoteFn  func(proof, issuerPublicKey, electionID, nullifier, signal []byte) bool
	DerivationVoteFn func(proof, issuerPublicKey, chainID, electionID, nullifier, signal []byte) bool
}

func (f Fake) VerifySignature(curve Curve, publicKey, msg, sig []byte) bool {
	if f.SignatureFn == nil {
		return false
	}
	return f.SignatureFn(curve, publicKey, msg, sig)
}

func (f Fake) VerifyWebAuthn(a WebAuthnAssertion) bool {
	if f.WebAuthnFn == nil {
		return false
	}
	return f.WebAuthnFn(a)
}

func (f Fake) VerifyBBSProof(proof, issuerPublicKey, nonce []byte) bool {
	if f.BBSProofFn == nil {
		return false
	}
	return f.BBSProofFn(proof, issuerPublicKey, nonce)
}

func (f Fake) VerifySemaphoreVote(proof, issuerPublicKey, electionID, nullifier, signal []byte) bool {
	if f.SemaphoreVoteFn == nil {
		return false
	}
	return f.SemaphoreVoteFn(proof, issuerPublicKey, electionID, nullifier, signal)
}

func (f Fake) VerifyDerivationVote(proof, issuerPublicKey, chainID, electionID, nullifier, signal []byte) bool {
	if f.DerivationVoteFn == nil {
		return false
	}
	return f.DerivationVoteFn(proof, issuerPublicKey, chainID, electionID, nullifier, signal)
}

// AcceptAll builds a Fake that accepts every verification.
func AcceptAll() Fake {
	return Fake{
		SignatureFn:      func(Curve, []byte, []byte, []byte) bool { return true },
		WebAuthnFn:       func(WebAuthnAssertion) bool { return true },
		BBSProofFn:       func([]byte, []byte, []byte) bool { return true },
		SemaphoreVoteFn:  func([]byte, []byte, []byte, []byte, []byte) bool { return true },
		DerivationVoteFn: func([]byte, []byte, []byte, []byte, []byte, []byte) bool { return true },
	}
}

// RejectAll builds a Fake that rejects everything.
func RejectAll() Fake { return Fake{} }
