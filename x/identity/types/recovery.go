// SPDX-License-Identifier: Apache-2.0

package types

import (
	"crypto/sha256"

	"cosmossdk.io/errors"
)

// Domain separators.
const (
	// GuardianCommitDomain domain-separates a guardian commitment.
	GuardianCommitDomain = "phi-guardian-commit-v1"
	// RecoveryIDDomain domain-separates the recovery-request identifier.
	RecoveryIDDomain = "phi-recovery-id-v1"
	// SocialRecoveryPoPDomain domain-separates the new key's proof-of-possession for a SOCIAL recovery.
	SocialRecoveryPoPDomain = "phi-social-recovery-v3"
	// ReauthRecoveryDomain domain-separates the re-authentication attestation a trusted issuer signs to authorise a REAUTH recovery.
	ReauthRecoveryDomain = "phi-recovery-reauth-v3"
)

// Sizes for the recovery/guardian primitives.
const (
	// GuardianCommitmentLen is the exact length of a guardian commitment (a SHA-256 digest).
	GuardianCommitmentLen = 32
	// GuardianSaltLen is the exact length of the secret that opens a guardian commitment.
	GuardianSaltLen = 32
	// RecoveryIDLen is the exact length of a recovery id (a SHA-256 digest).
	RecoveryIDLen = 32
	// MaxRecoveryNonceLen bounds the anti-replay nonce.
	MaxRecoveryNonceLen = 64
	// MaxRecoverySigLen bounds the proof-of-possession signature (DER ECDSA is < 80 bytes).
	MaxRecoverySigLen = 200
)

// GuardianCommitment derives the hiding commitment to a guardian: SHA256(domain ‖ 0x00 ‖ guardian_did ‖ 0x00 ‖ salt) The salt is a 32-byte secret held by the GUARDIAN (delivered out-of-band, never on chain).
func GuardianCommitment(guardianDID string, salt []byte) []byte {
	h := sha256.New()
	h.Write([]byte(GuardianCommitDomain))
	h.Write([]byte{0x00})
	h.Write([]byte(guardianDID))
	h.Write([]byte{0x00})
	h.Write(salt)
	return h.Sum(nil)
}

// DeriveRecoveryID derives a recovery request's identifier: SHA256(domain ‖ 0x00 ‖ did ‖ 0x00 ‖ proposed_new_pub_key ‖ 0x00 ‖ nonce) Binding the id to (did, key, nonce) makes it collision-free across concurrent requests and means a replayed nonce reproduces the same id, which the single-use nonce marker then rejects.
func DeriveRecoveryID(did string, proposedNewPubKey, nonce []byte) []byte {
	h := sha256.New()
	h.Write([]byte(RecoveryIDDomain))
	h.Write([]byte{0x00})
	h.Write([]byte(did))
	h.Write([]byte{0x00})
	h.Write(proposedNewPubKey)
	h.Write([]byte{0x00})
	h.Write(nonce)
	return h.Sum(nil)
}

// SocialRecoveryPoPMessage is the canonical message the NEW key signs to prove possession: CanonicalMessage(domain, chain_id, did, proposed_new_pub_key, new_controller, nonce) It binds the key to THIS network, THIS DID, THIS controller-to-be and THIS nonce, so an assertion captured for one recovery cannot be replayed into another (or retargeted at another DID).
func SocialRecoveryPoPMessage(chainID, did string, proposedNewPubKey []byte, newController string, nonce []byte) []byte {
	return CanonicalMessage(SocialRecoveryPoPDomain,
		[]byte(chainID),
		[]byte(did),
		proposedNewPubKey,
		[]byte(newController),
		nonce,
	)
}

// ReauthAttestationMessage is the canonical message a trusted issuer (phi-auth) signs to authorise a REAUTH recovery: CanonicalMessage(domain, chain_id, did, proposed_new_pub_key, new_controller, uniqueness_hash, nonce) chain_id binds the attestation to a single network: the same phi-auth key attesting on two Phi chains (e.g.
func ReauthAttestationMessage(chainID, did string, proposedNewPubKey []byte, newController string, uniquenessHash, nonce []byte) []byte {
	return CanonicalMessage(ReauthRecoveryDomain,
		[]byte(chainID),
		[]byte(did),
		proposedNewPubKey,
		[]byte(newController),
		uniquenessHash,
		nonce,
	)
}

// IsTerminalRecoveryStatus reports whether a status can never change again.
func IsTerminalRecoveryStatus(s RecoveryStatus) bool {
	return s != RECOVERY_STATUS_PENDING
}

// ValidateRecoveryNonce checks the anti-replay nonce's shape.
func ValidateRecoveryNonce(nonce []byte) error {
	if len(nonce) == 0 || len(nonce) > MaxRecoveryNonceLen {
		return errors.Wrapf(ErrInvalidRecovery, "nonce length %d (must be 1..%d)", len(nonce), MaxRecoveryNonceLen)
	}
	return nil
}
