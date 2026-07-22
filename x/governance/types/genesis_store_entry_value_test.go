// SPDX-License-Identifier: Apache-2.0

package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/governance/types"
)

func TestGenesis_RejectsAMalformedStoreEntryValue(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry types.StoreEntry
	}{
		{
			name: "short tally count",
			entry: types.StoreEntry{
				Key: types.TallyCountKey(1, 1), Value: []byte{0x01},
			},
		},
		{
			name: "short turnout",
			entry: types.StoreEntry{
				Key: types.TallyTurnoutKey(1), Value: []byte{0x01, 0x02},
			},
		},
		{
			name: "wide counted-vote marker",
			entry: types.StoreEntry{
				Key: types.CountedVoteKey(1, []byte("voter-aaaaaaaaaaaaaa")), Value: []byte{0x00, 0x01},
			},
		},
		{
			name: "empty counted-vote marker",
			entry: types.StoreEntry{
				Key: types.CountedVoteKey(1, []byte("voter-aaaaaaaaaaaaaa")), Value: nil,
			},
		},
		{
			name: "truncated frozen basis",
			entry: types.StoreEntry{
				Key: types.ProposalEligibilityKey(1), Value: make([]byte, 20),
			},
		},
		{
			name: "empty prune marker",
			entry: types.StoreEntry{
				Key: types.PruneKey(1), Value: nil,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gs := types.DefaultGenesis()
			gs.StoreEntries = []types.StoreEntry{tc.entry}
			require.Error(t, gs.Validate())
		})
	}
}

// The entries the module itself produces must pass — including a frozen basis in its LEGACY 16-byte form, which the keeper still reads.
func TestGenesis_AcceptsWellFormedStoreEntryValues(t *testing.T) {
	gs := types.DefaultGenesis()
	gs.StoreEntries = []types.StoreEntry{
		{Key: types.TallyCountKey(1, 1), Value: make([]byte, 8)},
		{Key: types.TallyTurnoutKey(1), Value: make([]byte, 8)},
		{Key: types.CountedVoteKey(1, []byte("voter-aaaaaaaaaaaaaa")), Value: []byte{0x01}},
		{Key: types.CountedVoteKey(1, []byte("voter-bbbbbbbbbbbbbb")), Value: []byte{types.IneligibleVoteMarker}},
		{Key: types.ProposalEligibilityKey(1), Value: make([]byte, 24)},
		{Key: types.ProposalEligibilityKey(2), Value: make([]byte, 16)},
		{Key: types.PruneKey(3), Value: []byte{1}},
	}
	require.NoError(t, gs.Validate())
}
