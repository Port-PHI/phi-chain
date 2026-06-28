// SPDX-License-Identifier: Apache-2.0

package types

// Module constants and KVStore keys for x/disclosure.
//
// The module is verify-only: it holds no per-disclosure state, only its
// governance parameters.
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
