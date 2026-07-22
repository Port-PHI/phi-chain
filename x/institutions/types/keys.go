// SPDX-License-Identifier: Apache-2.0

package types

import (
	"bytes"
	"encoding/binary"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/internal/storeprefix"
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
	// AdminEpochPrefix is the prefix for the institution → admin-set epoch counter.
	AdminEpochPrefix = []byte{0x70}
	// RedeemSubjectPrefix is the prefix for the NETWORK-WIDE daily redeem counter: (day, subject) → uphi redeemed.
	RedeemSubjectPrefix = []byte{0x80}
	// CounterPruneCursorPrefix is the single-key ring cursor for the CounterPrefix (0x40) prune sweep: it stores the raw byte key last examined.
	CounterPruneCursorPrefix = []byte{0x90}
	// RemovalQueuePrefix is the drain queue of institutions whose removal has been requested and whose ranged per-institution records are still being purged.
	RemovalQueuePrefix = []byte{0xB0}
)

// ResidualStorePrefixes are the live store prefixes with no structured genesis representation of their own, round-tripped verbatim through GenesisState.StoreEntries.
var ResidualStorePrefixes = [][]byte{
	AdminEpochPrefix,
	RedeemSubjectPrefix,
	HolderKycTierPrefix,
	LastAttestorPrefix,
}

// PerInstitutionRangePrefixes returns the iteration prefixes of every RANGED keyspace owned by one institution — the keyspaces holding many records per institution rather than a single one.
func PerInstitutionRangePrefixes(instID string) [][]byte {
	return [][]byte{
		RolePrefixFor(instID),
		ApprovalInstitutionPrefixFor(instID),
		HolderKycTierPrefixFor(instID),
	}
}

// ApprovalInstitutionPrefixFor returns the prefix for iterating EVERY approval of an institution, across all content hashes.
func ApprovalInstitutionPrefixFor(instID string) []byte {
	return append(append([]byte{}, ApprovalPrefix...), lenPrefixedID(instID)...)
}

// HolderKycTierPrefixFor returns the prefix for iterating every KYC tier assignment an institution has granted.
func HolderKycTierPrefixFor(instID string) []byte {
	return append(append([]byte{}, HolderKycTierPrefix...), lenPrefixedID(instID)...)
}

// IsResidualStoreKey reports whether a raw key lies strictly under one of the residual prefixes.
func IsResidualStoreKey(key []byte) bool {
	for _, p := range ResidualStorePrefixes {
		if len(key) > len(p) && string(key[:len(p)]) == string(p) {
			return true
		}
	}
	return false
}

// DepositMarkerByte is the one-byte sentinel value stored under a DepositPrefix key.
const DepositMarkerByte = byte(0x01)

// Redeem-subject kinds.
const (
	// RedeemSubjectDID marks a counter key whose subject is a resolved DID.
	RedeemSubjectDID = byte(0x01)
	// RedeemSubjectUnidentified marks the SHARED counter key used when no ACTIVE DID resolves for the holder.
	RedeemSubjectUnidentified = byte(0x02)
)

// HolderKycTierPrefix is the prefix for the (institution ‖ holder) → KYC tier assignment.
var HolderKycTierPrefix = []byte{0xA0}

// HolderKycTierKey builds the store key for one holder's KYC tier at one institution.
func HolderKycTierKey(instID string, holder sdk.AccAddress) []byte {
	k := append(append([]byte{}, HolderKycTierPrefix...), lenPrefixedID(instID)...)
	return append(k, holder.Bytes()...)
}

// LastAttestorPrefix is the prefix for the institution → address that published its current reserve attestation.
var LastAttestorPrefix = []byte{0xA1}

// LastAttestorKey builds the store key for an institution's most recent attestor.
func LastAttestorKey(instID string) []byte {
	return append(append([]byte{}, LastAttestorPrefix...), []byte(instID)...)
}

// MinAdminsForMint is the number of DISTINCT admin keys an institution must hold before it may mint.
const MinAdminsForMint = uint32(2)

// UnidentifiedRedeemSubject is the fixed subject string every holder without a resolvable ACTIVE DID shares.
const UnidentifiedRedeemSubject = "unidentified"

// RedeemSubjectCounterKey returns the network-wide daily redeem counter key for one subject.
func RedeemSubjectCounterKey(day int64, kind byte, subject string) []byte {
	k := append(append([]byte{}, RedeemSubjectPrefix...), i64be(day)...)
	k = append(k, kind)
	return append(k, []byte(subject)...)
}

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

// Bounds on free-form mint/redeem fields.
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

func lenPrefixedID(id string) []byte {
	b := []byte(id)
	return append([]byte{byte(len(b))}, b...)
}

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

// CounterRetentionDays is the number of past UTC days of daily cap counters kept before pruning.
const CounterRetentionDays = int64(2)

// CounterPruneBudget bounds the number of counter keys EXAMINED (and therefore at most deleted) per block by each family's sweep, keeping per-block work O(1) regardless of the stale-set size.
const CounterPruneBudget = 256

// CounterPruneCursorKey returns the key of the single-key ring cursor for the CounterPrefix sweep.
func CounterPruneCursorKey() []byte {
	return append([]byte{}, CounterPruneCursorPrefix...)
}

// RedeemSubjectDayBound returns the exclusive upper-bound key for iterating redeem-subject counters strictly older than `day`: the range [RedeemSubjectPrefix, RedeemSubjectPrefix‖i64be(day)).
func RedeemSubjectDayBound(day int64) []byte {
	return append(append([]byte{}, RedeemSubjectPrefix...), i64be(day)...)
}

// IsPrunableCounterKind reports whether a CounterPrefix kind is a daily cap counter the sweep may delete.
func IsPrunableCounterKind(kind string) bool {
	switch kind {
	case "md", "mu", "rd", "ru":
		return true
	default:
		return false
	}
}

// ParseCounterKeyDay decodes a CounterPrefix (0x40) key into its kind and day components; ok is false if the bytes are not a well-formed counter key.
func ParseCounterKeyDay(key []byte) (kind string, day int64, ok bool) {
	if len(key) == 0 || key[0] != CounterPrefix[0] {
		return "", 0, false
	}
	p := 1
	if p >= len(key) {
		return "", 0, false
	}
	l1 := int(key[p])
	p++
	if p+l1 > len(key) {
		return "", 0, false
	}
	p += l1 // skip instID
	if p >= len(key) {
		return "", 0, false
	}
	l2 := int(key[p])
	p++
	if p+l2 > len(key) {
		return "", 0, false
	}
	kind = string(key[p : p+l2])
	p += l2
	if p+8 > len(key) {
		return "", 0, false
	}
	day = int64(binary.BigEndian.Uint64(key[p : p+8]))
	return kind, day, true
}

// RemovalPruneBudget bounds the number of ranged per-institution records the removal sweep DELETES per block (plus an O(1) queue-finalisation write), keeping per-block work constant regardless of how many KYC-tier / role / approval records a removed institution holds.
const RemovalPruneBudget = 512

// RemovalQueueMarkerByte is the one-byte sentinel stored under a RemovalQueueKey.
const RemovalQueueMarkerByte = byte(0x01)

// RemovalQueueKey builds the queue key for an institution whose removal is draining.
func RemovalQueueKey(id string) []byte {
	return append(append([]byte{}, RemovalQueuePrefix...), lenPrefixedID(id)...)
}

// ParseRemovalQueueKey recovers the institution id from a RemovalQueueKey; ok is false if the bytes are not a well-formed queue key (prefix followed by a length-prefixed id).
func ParseRemovalQueueKey(key []byte) (string, bool) {
	return IDFromLenPrefixedKey(key, RemovalQueuePrefix)
}

// IDFromLenPrefixedKey recovers the length-prefixed institution id that immediately follows `prefix` in a composite per-institution key — RolePrefix, ApprovalPrefix, HolderKycTierPrefix and RemovalQueuePrefix all place lenPrefixedID(id) right after their one-byte prefix.
func IDFromLenPrefixedKey(key, prefix []byte) (string, bool) {
	if len(key) < len(prefix)+1 || !bytes.HasPrefix(key, prefix) {
		return "", false
	}
	p := len(prefix)
	l := int(key[p])
	p++
	if p+l > len(key) {
		return "", false
	}
	return string(key[p : p+l]), true
}

// DepositKey returns the anti-replay marker key for a deposit/redeem (direction: "mint"/"redeem").
func DepositKey(instID, direction, ref string) []byte {
	k := append(append([]byte{}, DepositPrefix...), lenPrefixedID(instID)...)
	k = append(k, byte(len(direction)))
	k = append(k, []byte(direction)...)
	return append(k, []byte(ref)...)
}

// AllStorePrefixes is the COMPLETE set of KVStore prefixes this module owns, and what genesis does with each.
func AllStorePrefixes() []storeprefix.Prefix {
	return []storeprefix.Prefix{
		{Name: "params", Bytes: ParamsKey},
		{Name: "institutions", Bytes: InstitutionPrefix},
		{Name: "role_grants", Bytes: RolePrefix},
		{Name: "approvals", Bytes: ApprovalPrefix},
		{Name: "cap_counters", Bytes: CounterPrefix},
		{Name: "deposit_markers", Bytes: DepositPrefix},
		{Name: "fx_requests", Bytes: FxRequestPrefix},
		{Name: "admin_epochs", Bytes: AdminEpochPrefix},
		{Name: "redeem_subject_counters", Bytes: RedeemSubjectPrefix},
		{Name: "holder_kyc_tiers", Bytes: HolderKycTierPrefix},
		{Name: "last_attestor", Bytes: LastAttestorPrefix},

		{Name: "counter_prune_cursor", Bytes: CounterPruneCursorPrefix,
			Carry: storeprefix.CarryDropped,
			Reason: "a resumable ring cursor over the cap counters: losing it restarts the sweep from " +
				"the start of the keyspace and costs nothing but work. It carries no authority and " +
				"gates no decision."},

		{Name: "removal_queue", Bytes: RemovalQueuePrefix,
			Carry: storeprefix.CarryDropped,
			Reason: "the drain queue for in-progress institution removals: a mid-drain removal is " +
				"exported as already-completed (its ranged role/approval/KYC records are filtered out and " +
				"the queue is not carried), so on import the id is fully removed and re-registerable with " +
				"no orphan granting state to inherit. It carries no authority and gates no consensus " +
				"decision."},
	}
}
