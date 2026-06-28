// SPDX-License-Identifier: Apache-2.0

package types

import "encoding/binary"

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
	// DefaultPhiToToman is the canonical fixed rate: 1 PHI = 100,000 toman.
	DefaultPhiToToman = 100_000
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
	// Copy the prefix into a fresh slice before appending so a future prefix with spare capacity can
	// never be aliased/mutated by append (matches the institutions keys idiom).
	return append(append([]byte{}, CoinAgePrefix...), []byte(address)...)
}

// MicroQuotaRetentionDays bounds how long daily micro-exemption quota keys are kept; BeginBlock prunes
// keys older than this. Far beyond the 1-day quota window, so live quotas are never touched.
const MicroQuotaRetentionDays = int64(7)

// MicroQuotaKey returns the key for an address's daily micro-exemption quota on a given day.
func MicroQuotaKey(day int64, address string) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(day))
	key := append(append([]byte{}, MicroQuotaPrefix...), buf...)
	return append(key, []byte(address)...)
}
