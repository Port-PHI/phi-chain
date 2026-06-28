// SPDX-License-Identifier: Apache-2.0

// Package phicrypto is the chain's cryptographic verification port: the single
// place where phi-chain relies on phi-crypto (through its C-ABI) for BBS+ /
// WebAuthn / sensitive signatures.
//
// Interface-first approach: all module logic (credentials/disclosure/voting) and
// the ante are coded against this interface and verified in tests with [Fake];
// the real cgo implementation (linked against libphi_crypto.a) is kept behind the
// `phicrypto_cgo` build tag so phi-chain stays "pure Go and offline" until the
// final integration step.
//
// Non-negotiable rule: never hand-roll cryptography. The only production
// implementation is the cgo wrapper over phi-crypto; the default tagless build
// ([Disabled]) rejects everything (fail-safe).
package phicrypto

// Curve is the signature curve — its code matches the phi-crypto C-ABI boundary exactly.
type Curve uint8

const (
	// Secp256k1 is the Cosmos default.
	Secp256k1 Curve = 0
	// Secp256r1 (P-256) — passkey/Secure Enclave and WebAuthn.
	Secp256r1 Curve = 1
)

// WebAuthnAssertion is the full input for verifying a WebAuthn (P-256) assertion.
//
// The signature is checked over the `AuthenticatorData ‖ SHA256(ClientDataJSON)`
// envelope; Challenge is the expected sign-doc/transaction hash, and Origin/RPID
// are the allowed domain (anti-phishing / anti-replay).
type WebAuthnAssertion struct {
	// AuthenticatorData (raw).
	AuthenticatorData []byte
	// ClientDataJSON (raw — the exact signed bytes).
	ClientDataJSON []byte
	// Signature is the ECDSA signature (DER or raw 64-byte).
	Signature []byte
	// Challenge is the expected challenge (raw bytes).
	Challenge []byte
	// PublicKey is the P-256 public key in SEC1 form.
	PublicKey []byte
	// Origin is the allowed domain (e.g. "https://portphi.com").
	Origin string
	// RPID is the relying-party id (e.g. "portphi.com").
	RPID string
}

// Verifier is the cryptographic verification port. Every method returns a
// fail-safe bool: any failure, bad input, or underlying error → false (the safe
// default at the consensus boundary).
type Verifier interface {
	// VerifySignature verifies an ECDSA signature on the given curve (high-S rejected).
	VerifySignature(curve Curve, publicKey, msg, sig []byte) bool
	// VerifyWebAuthn verifies a passkey assertion over AuthenticatorData‖SHA256(ClientDataJSON).
	VerifyWebAuthn(a WebAuthnAssertion) bool
	// VerifyBBSProof verifies a serialized BBS+ selective-disclosure proof against the issuer key and nonce.
	VerifyBBSProof(proof, issuerPublicKey, nonce []byte) bool
	// VerifySemaphoreVote verifies a BBS+ eligibility proof bound to (electionID, nullifier, signal):
	// the proof must have been produced against phi-crypto's bind_nonce(electionID, nullifier, signal),
	// so it is accepted only for the exact nullifier AND chosen option (signal) it was bound to
	// (anti-replay; one proof → at most one nullifier; non-malleable ballot). The binding hash lives
	// solely in phi-crypto (semaphore::bind_nonce) — never duplicated in Go.
	VerifySemaphoreVote(proof, issuerPublicKey, electionID, nullifier, signal []byte) bool
}
