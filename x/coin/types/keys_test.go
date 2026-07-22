// SPDX-License-Identifier: Apache-2.0

package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// IsGenesisStoreKey is the write-site guard the coin InitGenesis loop applies to every raw StoreEntry, so a key that never went through Validate (a migration/upgrade path) still cannot install itself outside the micro-quota keyspace.
func TestIsGenesisStoreKey(t *testing.T) {
	require.True(t, IsGenesisStoreKey(MicroQuotaKey(19_000, "phi1abc")),
		"a real micro-quota key must be accepted")

	require.False(t, IsGenesisStoreKey(MicroQuotaPrefix),
		"the bare prefix carries no record identity and must be refused")
	require.False(t, IsGenesisStoreKey(ParamsKey),
		"the params key would let a raw entry overwrite the params record")
	require.False(t, IsGenesisStoreKey(append(append([]byte(nil), CoinAgePrefix...), 'x')),
		"a key under a different owned prefix must be refused")
	require.False(t, IsGenesisStoreKey(nil), "an empty key must be refused")
}
