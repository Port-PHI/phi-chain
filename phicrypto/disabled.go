// SPDX-License-Identifier: Apache-2.0

//go:build !phicrypto_cgo

package phicrypto

// Disabled is the default Verifier implementation when the chain is built WITHOUT
// the `phicrypto_cgo` tag: no real cryptographic verification is available, so
// everything is rejected (fail-safe). This keeps phi-chain "pure Go and offline"
// until the final integration step; a production node must be built with
// `-tags phicrypto_cgo` (linking libphi_crypto) so that [CGO] replaces it.
type Disabled struct{}

// VerifySignature always returns false (fail-safe).
func (Disabled) VerifySignature(Curve, []byte, []byte, []byte) bool { return false }

// VerifyWebAuthn always returns false (fail-safe).
func (Disabled) VerifyWebAuthn(WebAuthnAssertion) bool { return false }

// VerifyBBSProof always returns false (fail-safe).
func (Disabled) VerifyBBSProof([]byte, []byte, []byte) bool { return false }

// VerifySemaphoreVote always returns false (fail-safe).
func (Disabled) VerifySemaphoreVote([]byte, []byte, []byte, []byte, []byte) bool { return false }

// Default returns a disabled Verifier in a build without the tag.
func Default() Verifier { return Disabled{} }

// DefaultEnforces reports whether the default verifier performs real cryptographic verification.
// It is false in the tagless build (Disabled rejects everything); the app uses it to refuse to run a
// live chain on a node that cannot verify consensus-critical proofs.
func DefaultEnforces() bool { return false }
