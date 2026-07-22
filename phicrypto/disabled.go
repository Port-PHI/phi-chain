// SPDX-License-Identifier: Apache-2.0

//go:build !phicrypto_cgo

package phicrypto

// Disabled is the default Verifier without the `phicrypto_cgo` tag: rejects everything (fail-safe); a production node must build with -tags phicrypto_cgo so [CGO] replaces it.
type Disabled struct{}

func (Disabled) VerifySignature(Curve, []byte, []byte, []byte) bool { return false }

func (Disabled) VerifyWebAuthn(WebAuthnAssertion) bool { return false }

func (Disabled) VerifyBBSProof([]byte, []byte, []byte) bool { return false }

func (Disabled) VerifySemaphoreVote([]byte, []byte, []byte, []byte, []byte) bool { return false }

func (Disabled) VerifyDerivationVote([]byte, []byte, []byte, []byte, []byte, []byte) bool {
	return false
}

// Default returns a disabled Verifier in a build without the tag.
func Default() Verifier { return Disabled{} }

// DefaultEnforces reports whether the default verifier performs real crypto verification (false in the tagless build; the app refuses to run a live chain on it).
func DefaultEnforces() bool { return false }
