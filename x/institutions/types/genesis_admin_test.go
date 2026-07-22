// SPDX-License-Identifier: Apache-2.0

package types_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

func adminAddr(label string) string {
	b := make([]byte, 20)
	copy(b, label)
	return sdk.AccAddress(b).String()
}

func genesisWithAdmin(admin string) types.GenesisState {
	gs := types.DefaultGenesis()
	gs.Institutions = []types.Institution{{
		Id: "bank-a", Admin: admin,
		InstitutionType: types.INSTITUTION_TYPE_FINANCIAL,
		VaultBalance:    "0", AttestedReserve: "0",
	}}
	return *gs
}

// TestGenesisAdmin_RejectsAnAdminThatIsNotARealKey walks every shape of non-key an admin field can hold.
func TestGenesisAdmin_RejectsAnAdminThatIsNotARealKey(t *testing.T) {
	for _, tc := range []struct {
		name  string
		admin string
	}{
		{"empty", ""},
		{"whitespace", "   "},
		{"not bech32", "not-an-address"},
		{"wrong prefix", "cosmos1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq"},
		{"truncated bech32", adminAddr("truncate-me_________")[:20]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gs := genesisWithAdmin(tc.admin)
			require.Error(t, gs.Validate(),
				"an admin that is not a real key must not be importable")
		})
	}
}

// A real admin passes.
func TestGenesisAdmin_AcceptsARealKey(t *testing.T) {
	require.NoError(t, genesisWithAdmin(adminAddr("real-root-admin_____")).Validate())
}

// The phantom is only dangerous because of what it lets through.
func TestGenesisAdmin_APhantomCannotSatisfyTheTwoAdminMintRule(t *testing.T) {
	real1 := adminAddr("real-admin-one______")

	gs := genesisWithAdmin("")
	gs.RoleGrants = []types.RoleGrant{
		{Institution: "bank-a", Address: real1, Role: types.INSTITUTION_ROLE_ADMIN},
	}
	require.Error(t, gs.Validate(),
		"one real key and a phantom must never be installable as the two admins minting requires")

	gs = genesisWithAdmin(real1)
	gs.RoleGrants = []types.RoleGrant{
		{Institution: "bank-a", Address: adminAddr("real-admin-two______"), Role: types.INSTITUTION_ROLE_ADMIN},
	}
	require.NoError(t, gs.Validate())
	require.Equal(t, uint32(2), types.MinAdminsForMint,
		"this test is written against a two-admin rule; revisit it if that changes")
}
