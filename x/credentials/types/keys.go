// SPDX-License-Identifier: Apache-2.0

package types

// Module constants and KVStore keys for x/credentials.
const (
	// ModuleName is the module name.
	ModuleName = "credentials"
	// StoreKey is the primary KVStore key.
	StoreKey = ModuleName
	// RouterKey is the message route.
	RouterKey = ModuleName
	// MaxBbsPubkeyLen bounds a template's issuer BBS public key (a state-bloat / malformed-input guard).
	// A real BBS-2023 (BLS12-381 G2) key is 96 bytes; this is a generous upper bound. The
	// cryptographic format is validated by phi-crypto at proof-verification time.
	MaxBbsPubkeyLen = 256
)

// KVStore key prefixes.
var (
	// ParamsKey is the single-record params key.
	ParamsKey = []byte{0x00}
	// TemplatePrefix prefixes id -> CredentialTemplate.
	TemplatePrefix = []byte{0x10}
	// AnchorPrefix prefixes credential_hash -> CredentialAnchor.
	AnchorPrefix = []byte{0x20}
	// AgreementPrefix prefixes hash -> Agreement.
	AgreementPrefix = []byte{0x30}
	// PersonalPrefix prefixes (owner_did, anchor_hash) -> PersonalAnchor.
	PersonalPrefix = []byte{0x40}
)

// TemplateKey builds the storage key for a credential template.
func TemplateKey(id string) []byte {
	return append(append([]byte{}, TemplatePrefix...), []byte(id)...)
}

// AnchorKey builds the storage key for a credential anchor.
func AnchorKey(credentialHash []byte) []byte {
	return append(append([]byte{}, AnchorPrefix...), credentialHash...)
}

// AgreementKey builds the storage key for an agreement.
func AgreementKey(hash []byte) []byte {
	return append(append([]byte{}, AgreementPrefix...), hash...)
}

// PersonalKey builds the composite storage key for a personal anchor:
// prefix || len(owner_did) || owner_did || anchor_hash. The owner DID is
// length-prefixed (one byte) so anchors group per owner for prefix iteration.
func PersonalKey(ownerDID string, anchorHash []byte) []byte {
	owner := []byte(ownerDID)
	key := append(append([]byte{}, PersonalPrefix...), byte(len(owner)))
	key = append(key, owner...)
	return append(key, anchorHash...)
}

// PersonalOwnerPrefix builds the iteration prefix for one owner's personal anchors.
func PersonalOwnerPrefix(ownerDID string) []byte {
	owner := []byte(ownerDID)
	key := append(append([]byte{}, PersonalPrefix...), byte(len(owner)))
	return append(key, owner...)
}
