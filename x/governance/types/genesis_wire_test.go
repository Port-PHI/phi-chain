// SPDX-License-Identifier: Apache-2.0

package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The genesis message carries the module's in-flight proposal state as raw key/value records, so its wire codec is what an export→import actually rides on.
func TestGenesisWire_StoreEntriesRoundTrip(t *testing.T) {
	gs := GenesisState{
		Params: DefaultParams(),
		StoreEntries: []StoreEntry{
			{Key: ProposalEligibilityKey(7), Value: []byte("twenty-four-bytes-of-basis")},
			{Key: TallyTurnoutKey(7), Value: []byte{0, 0, 0, 0, 0, 0, 0, 4}},
			{Key: CountedVoteKey(7, []byte("voter")), Value: []byte{1}},
			{Key: PruneKey(8), Value: []byte{1}},
		},
	}

	bz, err := gs.Marshal()
	require.NoError(t, err)
	require.Equal(t, gs.Size(), len(bz), "Size must agree with what Marshal produced")

	var back GenesisState
	require.NoError(t, back.Unmarshal(bz))
	require.Equal(t, gs, back)
}

// A zero-length value is indistinguishable from an absent one on the wire — standard proto3 for a bytes field.
func TestGenesisWire_EmptyValueIsNotDistinguishable(t *testing.T) {
	gs := GenesisState{
		Params:       DefaultParams(),
		StoreEntries: []StoreEntry{{Key: PruneKey(1), Value: []byte{}}},
	}
	bz, err := gs.Marshal()
	require.NoError(t, err)

	var back GenesisState
	require.NoError(t, back.Unmarshal(bz))
	require.Nil(t, back.StoreEntries[0].Value,
		"an empty value decodes as absent; no governance record may rely on the difference")
}
