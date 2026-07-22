// SPDX-License-Identifier: Apache-2.0

package types_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

const genesisTime int64 = 1_700_000_000

type timestampField struct {
	name string
	set  func(gs *types.GenesisState, v int64)
}

func genesisTimestampFields() []timestampField {
	return []timestampField{
		{
			name: "params.emergency_redemption.started_at",
			set:  func(gs *types.GenesisState, v int64) { gs.Params.EmergencyRedemption.StartedAt = v },
		},
		{
			name: "institution.last_attested_at",
			set: func(gs *types.GenesisState, v int64) {
				gs.Institutions = []types.Institution{validInstitution(v)}
			},
		},
	}
}

func validInstitution(lastAttestedAt int64) types.Institution {
	return types.Institution{
		Id:              "bank-a",
		Admin:           sdk.AccAddress([]byte("time-test-admin_____")).String(),
		InstitutionType: types.INSTITUTION_TYPE_FINANCIAL,
		VaultBalance:    "0",
		AttestedReserve: "0",
		LastAttestedAt:  lastAttestedAt,
	}
}

func timeTestGenesis(t *testing.T) *types.GenesisState {
	t.Helper()
	gs := types.DefaultGenesis()
	gs.Params.PhiToToman = 100_000
	gs.Params.RedeemFloorPerTx = "100"
	require.NoError(t, gs.Params.Validate())
	return gs
}

// TestGenesis_RejectsEveryFutureTimestamp walks EVERY timestamp field a genesis can set and asserts each one is refused when dated ahead of the genesis block time, accepted when zero or in the past, and refused when negative.
func TestGenesis_RejectsEveryFutureTimestamp(t *testing.T) {
	for _, f := range genesisTimestampFields() {
		t.Run(f.name, func(t *testing.T) {
			for _, tc := range []struct {
				name    string
				value   int64
				wantErr bool
			}{
				{"zero (never set)", 0, false},
				{"well in the past", genesisTime - 86_400, false},
				{"exactly the genesis block time", genesisTime, false},
				{"one second in the future", genesisTime + 1, true},
				{"far in the future", genesisTime + 100*365*86_400, true},
				{"negative", -1, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					gs := timeTestGenesis(t)
					f.set(gs, tc.value)
					err := gs.ValidateAtTime(genesisTime)
					if tc.wantErr {
						require.Error(t, err, "%s = %d must be rejected", f.name, tc.value)
						return
					}
					require.NoError(t, err)
				})
			}
		})
	}
}

// A negative timestamp is refused by the stateless Validate too — it is not a time the chain could ever have stamped, and it needs no block time to recognise.
func TestGenesis_ValidateRejectsNegativeTimestamps(t *testing.T) {
	for _, f := range genesisTimestampFields() {
		t.Run(f.name, func(t *testing.T) {
			gs := timeTestGenesis(t)
			f.set(gs, -1)
			require.Error(t, gs.Validate())

			gs = timeTestGenesis(t)
			f.set(gs, genesisTime)
			require.NoError(t, gs.Validate(), "the stateless path cannot judge a future value")
		})
	}
}
