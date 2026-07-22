// SPDX-License-Identifier: Apache-2.0

package types

import (
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/internal/storeentry"
)

// DefaultGenesis returns the default genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:       DefaultParams(),
		CoinAges:     []CoinAge{},
		StoreEntries: []StoreEntry{},
	}
}

func storeEntryRules() []storeentry.Rule {
	return []storeentry.Rule{
		{Name: "micro-exemption quota", Prefix: MicroQuotaPrefix, Value: storeentry.Uint64NoOverflow()},
	}
}

// Validate checks the genesis state for correctness.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, ca := range gs.CoinAges {
		// A malformed genesis must not seed a non-bech32 owner or a lot queue the FIFO penalty math would later read as garbage.
		if _, err := sdk.AccAddressFromBech32(ca.Address); err != nil {
			return fmt.Errorf("coin_age: invalid address %q: %w", ca.Address, err)
		}
		if seen[ca.Address] {
			return fmt.Errorf("duplicate coin_age entry for %s", ca.Address)
		}
		seen[ca.Address] = true

		// The bound is a consensus rule, not a runtime convenience: genesis must not seed a queue the keeper would refuse to grow.
		if uint32(len(ca.Lots)) > gs.Params.MaxCoinAgeLots {
			return fmt.Errorf("coin_age %s: %d lots exceeds max_coin_age_lots=%d",
				ca.Address, len(ca.Lots), gs.Params.MaxCoinAgeLots)
		}
		var prev int64
		for i, lot := range ca.Lots {
			if err := validNonNegAmount("lot amount", ca.Address, lot.Amount); err != nil {
				return err
			}
			if lot.AcquiredAt < 0 {
				return fmt.Errorf("coin_age %s: lot %d has a negative acquired_at", ca.Address, i)
			}
			// Oldest-first is the invariant every FIFO operation relies on.
			if i > 0 && lot.AcquiredAt < prev {
				return fmt.Errorf("coin_age %s: lots must be ordered oldest-first (lot %d is older than lot %d)",
					ca.Address, i, i-1)
			}
			prev = lot.AcquiredAt
		}
	}

	kvs := make([]storeentry.KV, len(gs.StoreEntries))
	for i, e := range gs.StoreEntries {
		kvs[i] = storeentry.KV{Key: e.Key, Value: e.Value}
	}
	return storeentry.Validate("store_entries", kvs, storeEntryRules()...)
}

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
