// SPDX-License-Identifier: Apache-2.0

package types

import (
	"encoding/binary"

	"github.com/Port-PHI/phi-chain/internal/storeprefix"
)

// coin module constants and keys.
const (
	// ModuleName is the module name.
	ModuleName = "coin"
	// StoreKey is the KVStore key.
	StoreKey = ModuleName
	// RouterKey is the message route.
	RouterKey = ModuleName

	// Denom is the base unit of the Phi coin.
	Denom = "uphi"
	// UphiPerPhi is the conversion ratio: 1 PHI = 1,000,000 uphi (exponent 6).
	UphiPerPhi = 1_000_000

	// RevenueAccountName is the module account that accrues the network's company revenue share.
	RevenueAccountName = "phi_revenue"
)

// Key prefixes.
var (
	// ParamsKey is the key for the single params record.
	ParamsKey = []byte{0x00}
	// CoinAgePrefix is the prefix for the address → CoinAge mapping.
	CoinAgePrefix = []byte{0x10}
	// MicroQuotaPrefix is the prefix for the daily micro-exemption quota: (day|address) → count.
	MicroQuotaPrefix = []byte{0x11}
)

// CoinAgeKey returns the key for an address's coin-age buckets.
func CoinAgeKey(address string) []byte {
	// Copy the prefix into a fresh slice before appending so a future prefix with spare capacity can never be aliased/mutated by append (matches the institutions keys idiom).
	return append(append([]byte{}, CoinAgePrefix...), []byte(address)...)
}

// MicroQuotaRetentionDays bounds how long daily micro-exemption quota keys are kept; BeginBlock prunes keys older than this.
const MicroQuotaRetentionDays = int64(7)

// MicroQuotaPruneBudget bounds the number of stale micro-exemption quota keys deleted per block, so a day-boundary rollover can never do O(keyset) deletes in a single block even if an adversary inflates one day's (day, address) keyset.
const MicroQuotaPruneBudget = 256

// MicroQuotaKey returns the key for an address's daily micro-exemption quota on a given day.
func MicroQuotaKey(day int64, address string) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(day))
	key := append(append([]byte{}, MicroQuotaPrefix...), buf...)
	return append(key, []byte(address)...)
}

// MicroQuotaDayBound returns the exclusive upper-bound key for iterating micro-exemption quota keys strictly older than `day`: the range [MicroQuotaPrefix, MicroQuotaPrefix‖be(day)).
func MicroQuotaDayBound(day int64) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(day))
	return append(append([]byte{}, MicroQuotaPrefix...), buf...)
}

// IsGenesisStoreKey reports whether a raw genesis store key lies strictly under the one prefix the raw StoreEntries exist to carry (the micro-exemption quota).
func IsGenesisStoreKey(key []byte) bool {
	p := MicroQuotaPrefix
	return len(key) > len(p) && string(key[:len(p)]) == string(p)
}

// AllStorePrefixes is the COMPLETE set of KVStore prefixes this module owns.
func AllStorePrefixes() []storeprefix.Prefix {
	return []storeprefix.Prefix{
		{Name: "params", Bytes: ParamsKey},
		{Name: "coin_ages", Bytes: CoinAgePrefix},
		{Name: "micro_quota", Bytes: MicroQuotaPrefix},
	}
}
