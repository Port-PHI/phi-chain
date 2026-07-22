// SPDX-License-Identifier: Apache-2.0

package types

import (
	"encoding/hex"
	"fmt"
)

// DefaultGenesis returns the default genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:    DefaultParams(),
		Elections: []Election{},
		Ballots:   []Ballot{},
	}
}

// Validate checks the genesis state for consistency.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}

	elections := make(map[string]Election)
	for _, e := range gs.Elections {
		if e.Id == "" {
			return fmt.Errorf("election with empty id")
		}
		if _, dup := elections[e.Id]; dup {
			return fmt.Errorf("duplicate election id in genesis: %s", e.Id)
		}
		if len(e.Options) < 2 {
			return fmt.Errorf("election %s must have at least two options", e.Id)
		}
		if len(e.OptionTallies) != len(e.Options) {
			return fmt.Errorf("election %s option_tallies length must match options", e.Id)
		}
		if e.VotingStart < 0 {
			return fmt.Errorf("election %s has negative voting_start", e.Id)
		}
		if e.VotingEnd <= e.VotingStart {
			return fmt.Errorf("election %s voting_end must be after voting_start", e.Id)
		}
		elections[e.Id] = e
	}

	// Recompute tallies from ballots so genesis cannot persist disagreeing counts.
	computed := make(map[string][]uint64)
	seenBallot := make(map[string]bool)
	for _, b := range gs.Ballots {
		e, ok := elections[b.ElectionId]
		if !ok {
			return fmt.Errorf("ballot references unknown election %s", b.ElectionId)
		}
		if len(b.Nullifier) == 0 {
			return fmt.Errorf("ballot with empty nullifier in election %s", b.ElectionId)
		}
		if int(b.OptionIndex) >= len(e.Options) {
			return fmt.Errorf("ballot in election %s has out-of-range option_index %d", b.ElectionId, b.OptionIndex)
		}
		key := b.ElectionId + "/" + hex.EncodeToString(b.Nullifier)
		if seenBallot[key] {
			return fmt.Errorf("duplicate ballot (nullifier reused) in genesis: %s", key)
		}
		seenBallot[key] = true

		if computed[b.ElectionId] == nil {
			computed[b.ElectionId] = make([]uint64, len(e.Options))
		}
		computed[b.ElectionId][b.OptionIndex]++
	}

	for id, e := range elections {
		var total uint64
		for i := range e.Options {
			var got uint64
			if c := computed[id]; c != nil {
				got = c[i]
			}
			if e.OptionTallies[i] != got {
				return fmt.Errorf("election %s option_tallies[%d]=%d disagrees with ballots (%d)", id, i, e.OptionTallies[i], got)
			}
			total += got
		}
		if e.TotalVotes != total {
			return fmt.Errorf("election %s total_votes=%d disagrees with ballots (%d)", id, e.TotalVotes, total)
		}
	}
	return nil
}
