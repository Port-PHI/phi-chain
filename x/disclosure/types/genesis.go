// SPDX-License-Identifier: Apache-2.0

package types

// DefaultGenesis returns the default genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params: DefaultParams(),
	}
}

// Validate checks the genesis state.
func (gs GenesisState) Validate() error {
	return gs.Params.Validate()
}
