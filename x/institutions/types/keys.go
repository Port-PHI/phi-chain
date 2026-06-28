// SPDX-License-Identifier: Apache-2.0

package types

import (
	"encoding/binary"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// institutions module constants and keys.
const (
	// ModuleName is the module name.
	ModuleName = "institutions"
	// StoreKey is the KVStore key.
	StoreKey = ModuleName
	// RouterKey is the message route.
	RouterKey = ModuleName
)

// Key prefixes.
var (
	// ParamsKey is the key for the single params record.
	ParamsKey = []byte{0x00}
	// InstitutionPrefix is the prefix for the id → Institution mapping.
	InstitutionPrefix = []byte{0x10}
	// RolePrefix is the prefix for the (institution, address) → RoleGrant mapping (RBAC).
	RolePrefix = []byte{0x20}
	// ApprovalPrefix is the prefix for accumulated approvals of sensitive actions: (institution, content hash, address) → marker.
	ApprovalPrefix = []byte{0x30}
	// CounterPrefix is the prefix for daily cap counters (institution, kind, day[, address]) → toman amount.
	CounterPrefix = []byte{0x40}
	// DepositPrefix is the prefix for the deposit/redeem anti-replay marker (institution, direction, ref) → marker.
	DepositPrefix = []byte{0x50}
	// FxRequestPrefix is the prefix for the fx_id → FxEntryRequest mapping (fx onboarding).
	FxRequestPrefix = []byte{0x60}
	// AdminEpochPrefix is the prefix for the institution → admin-set epoch counter. The epoch is bumped
	// whenever the institution's ADMIN set changes; pending multisig approvals are stamped with the
	// epoch they were cast under and stop counting once it advances (stale-approval freshness).
	AdminEpochPrefix = []byte{0x70}
)

// AdminEpochKey returns the key for an institution's admin-set epoch counter.
func AdminEpochKey(instID string) []byte {
	return append(append([]byte{}, AdminEpochPrefix...), lenPrefixedID(instID)...)
}

// FxRequestKey returns the store key for a pending fx onboarding request.
func FxRequestKey(fxID string) []byte {
	return append(append([]byte{}, FxRequestPrefix...), []byte(fxID)...)
}

// MaxInstitutionIDLen is the maximum length of an institution id (for length-prefixed keys).
const MaxInstitutionIDLen = 255

// MaxLicenseLen bounds the institution/fx license string (state-bloat guard).
const MaxLicenseLen = 1024

// Bounds on free-form mint/redeem fields. deposit_ref/redeem_ref are written into persistent KV
// keys (anti-replay markers) and the fx_* provenance fields into institution state/events; bounding
// them keeps key/value sizes predictable and prevents storage-bloat from oversized attacker input.
const (
	// MaxRefLen bounds a deposit_ref / redeem_ref (a bank/settlement reference).
	MaxRefLen = 128
	// MaxFxFieldLen bounds each fx provenance field (fx_currency / fx_amount / fx_tx_ref).
	MaxFxFieldLen = 128
)

// InstitutionKey returns the store key for an institution.
func InstitutionKey(id string) []byte {
	return append(append([]byte{}, InstitutionPrefix...), []byte(id)...)
}

// lenPrefixedID prefixes the institution id with a length byte so it is unambiguous within composite keys.
func lenPrefixedID(id string) []byte {
	b := []byte(id)
	return append([]byte{byte(len(b))}, b...)
}

// i64be converts an int64 to 8 big-endian bytes (day counter).
func i64be(v int64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(v))
	return b[:]
}

// RoleKey returns the key for an address's role within an institution.
func RoleKey(instID string, addr sdk.AccAddress) []byte {
	k := append(append([]byte{}, RolePrefix...), lenPrefixedID(instID)...)
	return append(k, addr.Bytes()...)
}

// RolePrefixFor returns the prefix for iterating all roles of an institution.
func RolePrefixFor(instID string) []byte {
	return append(append([]byte{}, RolePrefix...), lenPrefixedID(instID)...)
}

// ApprovalKey returns the key for one signer's approval of a sensitive action (content hash = fixed 32 bytes).
func ApprovalKey(instID string, contentHash []byte, signer sdk.AccAddress) []byte {
	k := append(append([]byte{}, ApprovalPrefix...), lenPrefixedID(instID)...)
	k = append(k, contentHash...)
	return append(k, signer.Bytes()...)
}

// ApprovalPrefixFor returns the prefix for iterating all approvals of a sensitive action.
func ApprovalPrefixFor(instID string, contentHash []byte) []byte {
	k := append(append([]byte{}, ApprovalPrefix...), lenPrefixedID(instID)...)
	return append(k, contentHash...)
}

// CounterTotalKey returns the daily institution-total counter key (kind: "md"/"rd" for mint/redeem).
func CounterTotalKey(instID, kind string, day int64) []byte {
	k := append(append([]byte{}, CounterPrefix...), lenPrefixedID(instID)...)
	k = append(k, byte(len(kind)))
	k = append(k, []byte(kind)...)
	return append(k, i64be(day)...)
}

// CounterUserKey returns the per-user daily counter key (kind: "mu"/"ru").
func CounterUserKey(instID, kind string, day int64, addr sdk.AccAddress) []byte {
	return append(CounterTotalKey(instID, kind, day), addr.Bytes()...)
}

// DepositKey returns the anti-replay marker key for a deposit/redeem (direction: "mint"/"redeem").
func DepositKey(instID, direction, ref string) []byte {
	k := append(append([]byte{}, DepositPrefix...), lenPrefixedID(instID)...)
	k = append(k, byte(len(direction)))
	k = append(k, []byte(direction)...)
	return append(k, []byte(ref)...)
}
