// SPDX-License-Identifier: Apache-2.0

package types

import "github.com/Port-PHI/phi-chain/internal/storeprefix"

// Module constants and KVStore keys for x/credentials.
const (
	ModuleName = "credentials"
	StoreKey   = ModuleName
	RouterKey  = ModuleName
	// MaxBbsPubkeyLen bounds a template's issuer BBS+ key (real BBS-2023 G2 key is 96 bytes).
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

// PersonalKey builds the composite key: prefix || len(owner_did) || owner_did || anchor_hash.
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

// AllStorePrefixes is the complete set of KVStore prefixes this module owns (genesis coverage tests work from it).
func AllStorePrefixes() []storeprefix.Prefix {
	return []storeprefix.Prefix{
		{Name: "params", Bytes: ParamsKey},
		{Name: "templates", Bytes: TemplatePrefix},
		{Name: "anchors", Bytes: AnchorPrefix},
		{Name: "agreements", Bytes: AgreementPrefix},
		{Name: "personal_anchors", Bytes: PersonalPrefix},
	}
}
