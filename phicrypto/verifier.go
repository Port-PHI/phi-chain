// SPDX-License-Identifier: Apache-2.0

// Package phicrypto is the chain's cryptographic verification port over phi-crypto's C-ABI.
package phicrypto

// Curve is the signature curve; its code matches the phi-crypto C-ABI boundary exactly.
type Curve uint8

const (
	// Secp256k1 is the Cosmos default.
	Secp256k1 Curve = 0
	// Secp256r1 (P-256) — passkey/Secure Enclave and WebAuthn.
	Secp256r1 Curve = 1
)

// WebAuthnAssertion is the full input for verifying a WebAuthn (P-256) assertion over AuthenticatorData‖SHA256(ClientDataJSON).
type WebAuthnAssertion struct {
	AuthenticatorData []byte
	// ClientDataJSON is the exact signed bytes.
	ClientDataJSON []byte
	// Signature is the ECDSA signature (DER or raw 64-byte).
	Signature []byte
	Challenge []byte
	// PublicKey is the P-256 public key in SEC1 form.
	PublicKey []byte
	// Origin is the allowed domain (e.g.
	Origin string
	// RPID is the relying-party id (e.g.
	RPID string
	// RequireUserVerification demands the authenticator's UV flag (biometric/PIN) for this tx, not merely User Presence; only this layer can enforce it.
	RequireUserVerification bool
}

// Verifier is the crypto verification port; every method returns a fail-safe bool (any failure/bad input → false at the consensus boundary).
type Verifier interface {
	// VerifySignature verifies an ECDSA signature on the given curve (high-S rejected).
	VerifySignature(curve Curve, publicKey, msg, sig []byte) bool
	// VerifyWebAuthn verifies a passkey assertion over AuthenticatorData‖SHA256(ClientDataJSON).
	VerifyWebAuthn(a WebAuthnAssertion) bool
	// VerifyBBSProof verifies a BBS+ selective-disclosure proof against the issuer key and nonce.
	VerifyBBSProof(proof, issuerPublicKey, nonce []byte) bool
	// VerifySemaphoreVote verifies a BBS+ eligibility proof bound to (electionID, nullifier, signal) via phi-crypto's bind_nonce.
	VerifySemaphoreVote(proof, issuerPublicKey, electionID, nullifier, signal []byte) bool
	// VerifyDerivationVote verifies the composed anonymous-vote (voting_snark) proof: accepting it means one-credential-one-nullifier-per-election (Sybil-resistant); chainID binds to one network; nullifier MUST equal the 48-byte compressed G1 bound inside proof.
	VerifyDerivationVote(proof, issuerPublicKey, chainID, electionID, nullifier, signal []byte) bool
}
