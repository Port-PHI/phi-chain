// SPDX-License-Identifier: Apache-2.0

package types

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Constants and store keys for the identity module.
const (
	// ModuleName is the module name.
	ModuleName = "identity"
	// StoreKey is the main KVStore key.
	StoreKey = ModuleName
	// RouterKey is the message route.
	RouterKey = ModuleName

	// DIDMethodPrefix is the canonical PHI DID method prefix.
	DIDMethodPrefix = "did:phi:"
	// MaxDIDLen bounds the DID string length (state-bloat and key-ambiguity guard).
	MaxDIDLen = 256
	// MaxPubKeyLen bounds the public-key byte length (P-256 SEC1 is 33/65 bytes; allow margin).
	MaxPubKeyLen = 200
	// MaxUniquenessHashLen bounds the uniqueness marker length (a hash, typically 32 bytes).
	MaxUniquenessHashLen = 64
	// MaxIssuerSigLen bounds the issuer signature length (DER ECDSA is < 80 bytes).
	MaxIssuerSigLen = 200
	// p256DIDTag is phi-crypto's curve tag for secp256r1 (P-256) in DID derivation (did.rs Curve::tag).
	p256DIDTag = 0x02
)

// DeriveDIDFromP256 reproduces phi-crypto's canonical DID derivation for a P-256 passkey:
// did:phi:<hex(first 20 bytes of SHA-256(0x02 ‖ pub_key))> (see phi-crypto src/did.rs did_from_public).
// The chain uses this to enforce that a registered DID self-certifies its public key.
func DeriveDIDFromP256(pubKey []byte) string {
	h := sha256.New()
	h.Write([]byte{p256DIDTag})
	h.Write(pubKey)
	digest := h.Sum(nil)
	return DIDMethodPrefix + hex.EncodeToString(digest[:20])
}

// ValidateDID checks the canonical PHI DID syntax: the "did:phi:" method prefix, a bounded
// length, a non-empty method-specific id, and only URL-safe identifier characters.
func ValidateDID(did string) error {
	if did == "" {
		return fmt.Errorf("did cannot be empty")
	}
	if len(did) > MaxDIDLen {
		return fmt.Errorf("did length %d exceeds %d", len(did), MaxDIDLen)
	}
	if !strings.HasPrefix(did, DIDMethodPrefix) {
		return fmt.Errorf("did must start with %q", DIDMethodPrefix)
	}
	id := did[len(DIDMethodPrefix):]
	if id == "" {
		return fmt.Errorf("did method-specific id cannot be empty")
	}
	for _, r := range id {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' || r == ':'
		if !ok {
			return fmt.Errorf("did contains an invalid character %q", r)
		}
	}
	return nil
}

// Key prefixes in the KVStore.
var (
	// ParamsKey is the single-record key for params.
	ParamsKey = []byte{0x00}
	// IdentityCountKey is the total identity counter (basis for the bootstrap lock).
	IdentityCountKey = []byte{0x01}
	// DIDPrefix is the prefix for the did → DIDDocument mapping.
	DIDPrefix = []byte{0x10}
	// UniquenessPrefix is the prefix for the uniqueness_hash → did mapping (enforces one-human-one-DID).
	UniquenessPrefix = []byte{0x11}
	// DIDToValidatorPrefix is the prefix for the did → valoper mapping (unique DID per validator).
	DIDToValidatorPrefix = []byte{0x12}
	// ValidatorToDIDPrefix is the prefix for the valoper → did mapping (used to release on validator removal).
	ValidatorToDIDPrefix = []byte{0x13}
	// TrustedIssuerPrefix is the prefix for the issuer_did → TrustedIssuer mapping (gov-managed registry).
	TrustedIssuerPrefix = []byte{0x14}
	// ControllerIndexPrefix is the prefix for the (controller ‖ did) → marker secondary index
	// (avoids O(n) scans when checking one-human-one-vote controller eligibility).
	ControllerIndexPrefix = []byte{0x15}
	// IssuerNoncePrefix is the prefix for the (issuer_did ‖ nonce) → marker set: each issuer
	// attestation nonce is single-use (anti-replay), so the same nonce cannot be reused by an issuer.
	IssuerNoncePrefix = []byte{0x16}
)

// IssuerNonceKey builds the (issuer_did ‖ 0x00 ‖ nonce) key. The 0x00 separator is unambiguous
// because a DID's characters never include a NUL byte (see ValidateDID).
func IssuerNonceKey(issuerDid string, nonce []byte) []byte {
	k := append(append([]byte{}, IssuerNoncePrefix...), []byte(issuerDid)...)
	k = append(k, 0x00)
	return append(k, nonce...)
}

// TrustedIssuerKey builds the store key for a trusted issuer.
func TrustedIssuerKey(did string) []byte {
	return append(append([]byte{}, TrustedIssuerPrefix...), []byte(did)...)
}

// ControllerIndexKey builds the (controller ‖ 0x00 ‖ did) secondary-index key. The 0x00 separator keeps
// the controller boundary unambiguous so a prefix scan returns exactly that controller's DIDs.
func ControllerIndexKey(controller, did string) []byte {
	k := append(append([]byte{}, ControllerIndexPrefix...), []byte(controller)...)
	k = append(k, 0x00)
	return append(k, []byte(did)...)
}

// ControllerIndexPrefixFor returns the iteration prefix for one controller's DIDs.
func ControllerIndexPrefixFor(controller string) []byte {
	k := append(append([]byte{}, ControllerIndexPrefix...), []byte(controller)...)
	return append(k, 0x00)
}

// DIDKey builds the store key for a DIDDocument.
// The prefix is copied first (never append directly onto the shared global slice) to avoid
// slice-aliasing corruption if the prefix is ever widened to a multi-byte literal.
func DIDKey(did string) []byte {
	return append(append([]byte{}, DIDPrefix...), []byte(did)...)
}

// UniquenessKey builds the key for a biometric uniqueness marker.
func UniquenessKey(hash []byte) []byte {
	return append(append([]byte{}, UniquenessPrefix...), hash...)
}

// DIDToValidatorKey builds the key for the did → valoper mapping.
func DIDToValidatorKey(did string) []byte {
	return append(append([]byte{}, DIDToValidatorPrefix...), []byte(did)...)
}

// ValidatorToDIDKey builds the key for the valoper → did mapping.
func ValidatorToDIDKey(valoper string) []byte {
	return append(append([]byte{}, ValidatorToDIDPrefix...), []byte(valoper)...)
}
