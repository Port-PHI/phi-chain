// SPDX-License-Identifier: Apache-2.0

package types

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/Port-PHI/phi-chain/internal/storeprefix"
	"github.com/Port-PHI/phi-chain/phicrypto"
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
)

// DeriveDIDFromP256 returns the canonical did:phi for a secp256r1 (P-256) passkey via the single phicrypto derivation port — did:phi:<hex(SHA-256(0x02 ‖ canonical-uncompressed-SEC1(pub_key)))>, the full 32-byte digest, byte-identical to phi-crypto src/did.rs did_from_public and pinned by the cross-language KAT (phicrypto/did_kat_test.go ↔ phi-crypto/tests/did_kat.rs).
func DeriveDIDFromP256(pubKey []byte) (string, error) {
	return phicrypto.DeriveDID(phicrypto.Secp256r1, pubKey)
}

// CurveForKeyType maps an identity's key type to the phicrypto curve that verifies its signatures and derives its DID.
func CurveForKeyType(keyType KeyType) (phicrypto.Curve, error) {
	switch keyType {
	case KEY_TYPE_UNSPECIFIED, KEY_TYPE_SECP256R1:
		return phicrypto.Secp256r1, nil
	case KEY_TYPE_SECP256K1:
		return phicrypto.Secp256k1, nil
	default:
		return 0, fmt.Errorf("unsupported key_type %s", keyType)
	}
}

// DeriveDIDForKeyType returns the canonical did:phi for a key on its own curve — the dual-curve derivation from Slice 0: did:phi:<hex(SHA-256(tag ‖ canonical-SEC1(pub_key)))>, where the tag is 0x01 for secp256k1 (compressed SEC1) and 0x02 for secp256r1 (uncompressed SEC1).
func DeriveDIDForKeyType(keyType KeyType, pubKey []byte) (string, error) {
	curve, err := CurveForKeyType(keyType)
	if err != nil {
		return "", err
	}
	return phicrypto.DeriveDID(curve, pubKey)
}

// ValidateDID checks the canonical PHI DID syntax: the "did:phi:" method prefix, a bounded length, a non-empty method-specific id, and only URL-safe identifier characters.
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
	// ControllerIndexPrefix is the prefix for the (controller ‖ did) → marker secondary index (avoids O(n) scans when checking one-human-one-vote controller eligibility).
	ControllerIndexPrefix = []byte{0x15}
	// IssuerNoncePrefix is the prefix for the (issuer_did ‖ nonce) → marker set: each issuer attestation nonce is single-use (anti-replay), so the same nonce cannot be reused by an issuer.
	IssuerNoncePrefix = []byte{0x16}
	// GuardiansPrefix is the prefix for the did → GuardianSet mapping (social-recovery guardians).
	GuardiansPrefix = []byte{0x17}
	// RecoveryPrefix is the prefix for the recovery_id → RecoveryRequest mapping.
	RecoveryPrefix = []byte{0x18}
	// RecoveryByDIDPrefix is the prefix for the (did ‖ 0x00 ‖ recovery_id) → marker index.
	RecoveryByDIDPrefix = []byte{0x19}
	// RecoveryNoncePrefix is the prefix for the (did ‖ 0x00 ‖ nonce) → marker set: each recovery nonce is single-use per DID (anti-replay), mirroring the issuer-nonce mechanism.
	RecoveryNoncePrefix = []byte{0x1A}

	// The three structures below are DERIVED state for the one-human-one-vote quorum denominator.

	// ControllerEligibilityPrefix is the prefix for the controller → oldest-non-revoked-DID created_at record.
	ControllerEligibilityPrefix = []byte{0x1B}
	// EligibilityByAgePrefix is the prefix for the (created_at ‖ controller) → marker mirror of the record above, ordered by age.
	EligibilityByAgePrefix = []byte{0x1C}
	// EligibleControllerTotalKey is the single-record count of controllers holding an eligibility record — the O(1) total the denominator subtracts the tail from.
	EligibleControllerTotalKey = []byte{0x1D}

	// GuardianEpochPrefix is the prefix for the did → guardian-set epoch counter, incremented every time a DID's guardian set is REPLACED.
	GuardianEpochPrefix = []byte{0x1E}
	// RecoveryTallyEpochPrefix is the prefix for the recovery_id → guardian-set epoch under which that request's approvals and rejections were collected.
	RecoveryTallyEpochPrefix = []byte{0x1F}

	// ControllerSweepPrefix is the prefix for the controller → sweep-status record: the controller's FIRST ACTIVE DID (key order, for binding) plus whether it holds any SUSPENDED or any REVOKED DID.
	ControllerSweepPrefix = []byte{0x20}
)

// MaxControllerDIDScan bounds how many of one controller's DIDs the (controller ‖ did) index is walked before the scan gives up.
const MaxControllerDIDScan = 64

// GuardianEpochKey builds the store key for a DID's guardian-set epoch.
func GuardianEpochKey(did string) []byte {
	return append(append([]byte{}, GuardianEpochPrefix...), []byte(did)...)
}

// RecoveryTallyEpochKey builds the store key for a request's tally epoch.
func RecoveryTallyEpochKey(recoveryID []byte) []byte {
	return append(append([]byte{}, RecoveryTallyEpochPrefix...), recoveryID...)
}

// ControllerEligibilityKey builds the store key for a controller's eligibility record.
func ControllerEligibilityKey(controller string) []byte {
	return append(append([]byte{}, ControllerEligibilityPrefix...), []byte(controller)...)
}

// EncodeControllerEligibility encodes a controller's eligibility record: the created_at of its OLDEST ACTIVE DID, and the time it most recently BECAME eligible.
func EncodeControllerEligibility(oldest, eligibleSince int64) []byte {
	return append(SortableInt64(oldest), SortableInt64(eligibleSince)...)
}

// DecodeControllerEligibility reads an eligibility record.
func DecodeControllerEligibility(b []byte) (oldest, eligibleSince int64, ok bool) {
	switch len(b) {
	case 8:
		return ParseSortableInt64(b), 0, true
	case 16:
		return ParseSortableInt64(b[:8]), ParseSortableInt64(b[8:]), true
	default:
		return 0, 0, false
	}
}

// EligibilityByAgeKey builds the (created_at ‖ controller) key of the age-ordered mirror.
func EligibilityByAgeKey(createdAt int64, controller string) []byte {
	k := append(append([]byte{}, EligibilityByAgePrefix...), SortableInt64(createdAt)...)
	return append(k, []byte(controller)...)
}

// CreatedAtFromEligibilityByAgeKey recovers created_at from a full age-ordered mirror key.
func CreatedAtFromEligibilityByAgeKey(key []byte) (int64, bool) {
	start := len(EligibilityByAgePrefix)
	if len(key) < start+8 {
		return 0, false
	}
	return ParseSortableInt64(key[start : start+8]), true
}

// SortableInt64 encodes a signed timestamp so that byte order matches numeric order: the sign bit is flipped, so negative values sort before non-negative ones.
func SortableInt64(v int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v)^(1<<63))
	return b
}

// ParseSortableInt64 is the inverse of SortableInt64.
func ParseSortableInt64(b []byte) int64 {
	if len(b) != 8 {
		panic(fmt.Sprintf("identity: sortable int64 must be 8 bytes, got %d", len(b)))
	}
	return int64(binary.BigEndian.Uint64(b) ^ (1 << 63))
}

// GuardiansKey builds the store key for a DID's guardian set.
func GuardiansKey(did string) []byte {
	return append(append([]byte{}, GuardiansPrefix...), []byte(did)...)
}

// RecoveryKey builds the store key for a recovery request.
func RecoveryKey(recoveryID []byte) []byte {
	return append(append([]byte{}, RecoveryPrefix...), recoveryID...)
}

// RecoveryByDIDKey builds the (did ‖ 0x00 ‖ recovery_id) index key.
func RecoveryByDIDKey(did string, recoveryID []byte) []byte {
	k := append(append([]byte{}, RecoveryByDIDPrefix...), []byte(did)...)
	k = append(k, 0x00)
	return append(k, recoveryID...)
}

// RecoveryByDIDPrefixFor returns the iteration prefix for one DID's recovery requests.
func RecoveryByDIDPrefixFor(did string) []byte {
	k := append(append([]byte{}, RecoveryByDIDPrefix...), []byte(did)...)
	return append(k, 0x00)
}

// RecoveryNonceKey builds the (did ‖ 0x00 ‖ nonce) single-use marker key.
func RecoveryNonceKey(did string, nonce []byte) []byte {
	k := append(append([]byte{}, RecoveryNoncePrefix...), []byte(did)...)
	k = append(k, 0x00)
	return append(k, nonce...)
}

// IssuerNonceKey builds the (issuer_did ‖ 0x00 ‖ nonce) key.
func IssuerNonceKey(issuerDid string, nonce []byte) []byte {
	k := append(append([]byte{}, IssuerNoncePrefix...), []byte(issuerDid)...)
	k = append(k, 0x00)
	return append(k, nonce...)
}

// TrustedIssuerKey builds the store key for a trusted issuer.
func TrustedIssuerKey(did string) []byte {
	return append(append([]byte{}, TrustedIssuerPrefix...), []byte(did)...)
}

// ControllerIndexKey builds the (controller ‖ 0x00 ‖ did) secondary-index key.
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

// Sweep-status flag bits, stored in the first byte of a ControllerSweep record.
const (
	sweepFlagHasSuspended byte = 1 << 0
	sweepFlagHasRevoked   byte = 1 << 1
)

// ControllerSweepKey builds the store key for a controller's sweep-status record.
func ControllerSweepKey(controller string) []byte {
	return append(append([]byte{}, ControllerSweepPrefix...), []byte(controller)...)
}

// EncodeControllerSweep encodes a controller's sweep-status record: a flags byte (any SUSPENDED, any REVOKED) followed by the controller's first ACTIVE DID in key order ("" when it has none).
func EncodeControllerSweep(activeDID string, hasSuspended, hasRevoked bool) []byte {
	var flags byte
	if hasSuspended {
		flags |= sweepFlagHasSuspended
	}
	if hasRevoked {
		flags |= sweepFlagHasRevoked
	}
	return append([]byte{flags}, []byte(activeDID)...)
}

// DecodeControllerSweep reads a sweep-status record.
func DecodeControllerSweep(b []byte) (activeDID string, hasSuspended, hasRevoked, ok bool) {
	if len(b) == 0 {
		return "", false, false, false
	}
	flags := b[0]
	return string(b[1:]), flags&sweepFlagHasSuspended != 0, flags&sweepFlagHasRevoked != 0, true
}

// DIDKey builds the store key for a DIDDocument.
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

// AllStorePrefixes is the COMPLETE set of KVStore prefixes this module owns, and what genesis does with each.
func AllStorePrefixes() []storeprefix.Prefix {
	const derivedFromReplay = "derived state, rebuilt on import by replaying every identity through " +
		"SetIdentity, which is its only writer; EligibilityIndexInvariant asserts the agreement"

	return []storeprefix.Prefix{
		{Name: "params", Bytes: ParamsKey},
		{Name: "identity_count", Bytes: IdentityCountKey},
		{Name: "identities", Bytes: DIDPrefix},
		{Name: "uniqueness_markers", Bytes: UniquenessPrefix},
		{Name: "did_to_validator", Bytes: DIDToValidatorPrefix},
		{Name: "validator_to_did", Bytes: ValidatorToDIDPrefix},
		{Name: "trusted_issuers", Bytes: TrustedIssuerPrefix},
		{Name: "controller_index", Bytes: ControllerIndexPrefix},
		{Name: "issuer_nonces", Bytes: IssuerNoncePrefix},
		{Name: "guardian_sets", Bytes: GuardiansPrefix},
		{Name: "recovery_requests", Bytes: RecoveryPrefix},
		{Name: "recovery_by_did", Bytes: RecoveryByDIDPrefix},
		{Name: "recovery_nonces", Bytes: RecoveryNoncePrefix},
		{Name: "guardian_epochs", Bytes: GuardianEpochPrefix},
		{Name: "recovery_tally_epochs", Bytes: RecoveryTallyEpochPrefix},

		// Carried, not merely derived — for one of its two fields.
		{Name: "controller_eligibility", Bytes: ControllerEligibilityPrefix},
		{Name: "eligibility_by_age", Bytes: EligibilityByAgePrefix,
			Carry: storeprefix.CarryDerived, Reason: derivedFromReplay},
		{Name: "eligible_controller_total", Bytes: EligibleControllerTotalKey,
			Carry: storeprefix.CarryDerived, Reason: derivedFromReplay},
		{Name: "controller_sweep_status", Bytes: ControllerSweepPrefix,
			Carry: storeprefix.CarryDerived, Reason: derivedFromReplay},
	}
}
