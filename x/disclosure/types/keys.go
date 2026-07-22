// SPDX-License-Identifier: Apache-2.0

package types

import "github.com/Port-PHI/phi-chain/internal/storeprefix"

// Module constants and KVStore keys for x/disclosure.
const (
	// ModuleName is the module name.
	ModuleName = "disclosure"
	// StoreKey is the primary KVStore key.
	StoreKey = ModuleName
	// RouterKey is the message route.
	RouterKey = ModuleName
)

// KVStore key prefixes.
var (
	// ParamsKey is the single-record params key.
	ParamsKey = []byte{0x00}
)

// AllStorePrefixes is the COMPLETE set of KVStore prefixes this module owns.
func AllStorePrefixes() []storeprefix.Prefix {
	return []storeprefix.Prefix{
		{Name: "params", Bytes: ParamsKey},
	}
}
