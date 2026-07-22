// SPDX-License-Identifier: Apache-2.0

package types

import "github.com/Port-PHI/phi-chain/internal/storeentry"

func DefaultGenesis() *GenesisState {
	return &GenesisState{Params: DefaultParams(), StoreEntries: []StoreEntry{}}
}

func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	// Each raw record must lie strictly under an exported prefix AND decode under it: these records are a vote tally read via a fixed-width decoder that reads a wrong width as a smaller number, so a malformed value silently changes a result rather than failing the import.
	kvs := make([]storeentry.KV, len(gs.StoreEntries))
	for i, e := range gs.StoreEntries {
		kvs[i] = storeentry.KV{Key: e.Key, Value: e.Value}
	}
	return storeentry.Validate("store_entries", kvs, storeEntryRules()...)
}

func storeEntryRules() []storeentry.Rule {
	return []storeentry.Rule{
		// Counters bounded below saturation: 0xFF×8 passes a width check but decodes to an impossible turnout.
		{Name: "running tally count", Prefix: TallyCountPrefix, Value: storeentry.Uint64NoOverflow()},
		{Name: "turnout", Prefix: TallyTurnoutPrefix, Value: storeentry.Uint64NoOverflow()},
		{Name: "counted-vote marker", Prefix: CountedVotePrefix, Value: storeentry.FixedLen(1)},
		// 24 bytes now; 16 is the legacy pre-frozen_at form the keeper still reads, so accept both.
		{Name: "frozen eligibility basis", Prefix: ProposalEligibilityPrefix, Value: storeentry.OneOfLen(16, 24)},
		{Name: "pruning-queue marker", Prefix: PrunePrefix, Value: storeentry.NonEmpty()},
	}
}
