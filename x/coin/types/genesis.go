// SPDX-License-Identifier: Apache-2.0

package types

import (
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultGenesis returns the default genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:   DefaultParams(),
		CoinAges: []CoinAge{},
	}
}

// Validate checks the genesis state for correctness.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, ca := range gs.CoinAges {
		// Validate the address and both age buckets (a malformed genesis must not seed a
		// non-bech32 owner or a negative/garbage bucket the demurrage math would later read).
		if _, err := sdk.AccAddressFromBech32(ca.Address); err != nil {
			return fmt.Errorf("coin_age: invalid address %q: %w", ca.Address, err)
		}
		if seen[ca.Address] {
			return fmt.Errorf("duplicate coin_age entry for %s", ca.Address)
		}
		seen[ca.Address] = true
		if err := validNonNegAmount("young_amount", ca.Address, ca.YoungAmount); err != nil {
			return err
		}
		if err := validNonNegAmount("old_amount", ca.Address, ca.OldAmount); err != nil {
			return err
		}
		if ca.YoungSince < 0 {
			return fmt.Errorf("coin_age %s: young_since must not be negative", ca.Address)
		}
	}
	return nil
}

// validNonNegAmount: an empty amount is allowed (= zero); otherwise it must be a non-negative integer.
func validNonNegAmount(field, addr, s string) error {
	if s == "" {
		return nil
	}
	v, ok := math.NewIntFromString(s)
	if !ok || v.IsNegative() {
		return fmt.Errorf("coin_age %s: invalid %s %q", addr, field, s)
	}
	return nil
}
