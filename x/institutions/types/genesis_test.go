// SPDX-License-Identifier: Apache-2.0

package types_test

import (
	"encoding/binary"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

func epoch8(e uint64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], e)
	return b[:]
}

// A genesis carrying well-formed marker values (canonical deposit sentinel, decimal cap counter, 8-byte approval epoch) round-trips through Validate; a malformed value in any of the three raw marker stores fails Validate cleanly instead of being seeded to panic or under-count later.
func TestGenesisValidate_MarkerValues(t *testing.T) {
	signer := sdk.AccAddress(make([]byte, 20))
	contentHash := make([]byte, 32)

	validDeposit := types.StoreEntry{Key: types.DepositKey("inst", "mint", "ref-1"), Value: []byte{types.DepositMarkerByte}}
	validCounter := types.StoreEntry{Key: types.CounterTotalKey("inst", "mint_total", 1), Value: []byte("1000")}
	validApproval := types.StoreEntry{Key: types.ApprovalKey("inst", contentHash, signer), Value: epoch8(3)}

	base := func() *types.GenesisState {
		gs := types.DefaultGenesis()
		gs.DepositMarkers = []types.StoreEntry{validDeposit}
		gs.CapCounters = []types.StoreEntry{validCounter}
		gs.Approvals = []types.StoreEntry{validApproval}
		return gs
	}

	require.NoError(t, base().Validate(), "a genesis with well-formed marker values must validate")

	cases := []struct {
		name   string
		mutate func(gs *types.GenesisState)
	}{
		{"deposit wrong sentinel", func(gs *types.GenesisState) {
			gs.DepositMarkers = []types.StoreEntry{{Key: validDeposit.Key, Value: []byte{0x02}}}
		}},
		{"deposit empty value", func(gs *types.GenesisState) {
			gs.DepositMarkers = []types.StoreEntry{{Key: validDeposit.Key, Value: nil}}
		}},
		{"counter not a number", func(gs *types.GenesisState) {
			gs.CapCounters = []types.StoreEntry{{Key: validCounter.Key, Value: []byte("not-a-number")}}
		}},
		{"counter negative", func(gs *types.GenesisState) {
			gs.CapCounters = []types.StoreEntry{{Key: validCounter.Key, Value: []byte("-5")}}
		}},
		{"approval wrong width", func(gs *types.GenesisState) {
			gs.Approvals = []types.StoreEntry{{Key: validApproval.Key, Value: []byte{0x00, 0x01, 0x02}}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gs := base()
			tc.mutate(gs)
			require.Error(t, gs.Validate(), "a malformed marker value must fail Validate")
		})
	}
}
