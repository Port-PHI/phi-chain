// SPDX-License-Identifier: Apache-2.0

package types_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

func residualGenesis(entries ...types.StoreEntry) types.GenesisState {
	gs := types.DefaultGenesis()
	gs.StoreEntries = entries
	return *gs
}

// TestResidualValues_MalformedValuesAreRejected walks every residual keyspace and every way its value can be wrong.
func TestResidualValues_MalformedValuesAreRejected(t *testing.T) {
	holder := sdk.AccAddress([]byte("residual-holder_____"))

	for _, tc := range []struct {
		name  string
		key   []byte
		value []byte
	}{
		{"admin epoch, empty", types.AdminEpochKey("bank-a"), nil},
		{"admin epoch, too short", types.AdminEpochKey("bank-a"), []byte{0, 0, 0, 1}},
		{"admin epoch, too long", types.AdminEpochKey("bank-a"), make([]byte, 9)},
		{"kyc tier, wrong width", types.HolderKycTierKey("bank-a", holder), []byte{0, 1}},
		{"last attestor, empty", types.LastAttestorKey("bank-a"), nil},
		{"last attestor, longer than any address", types.LastAttestorKey("bank-a"), make([]byte, 256)},
		{"redeem counter, not a number", types.RedeemSubjectCounterKey(19_000, 'd', "did:phi:x"), []byte("abc")},
		{"redeem counter, negative", types.RedeemSubjectCounterKey(19_000, 'd', "did:phi:x"), []byte("-5")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gs := residualGenesis(types.StoreEntry{Key: tc.key, Value: tc.value})
			require.Error(t, gs.Validate(),
				"a malformed residual value must be rejected, never read as a silent zero")
		})
	}
}

// Well-formed values pass, so the rule gates on the encoding rather than refusing the keyspace.
func TestResidualValues_WellFormedValuesAreAccepted(t *testing.T) {
	holder := sdk.AccAddress([]byte("residual-holder_____"))
	attestor := sdk.AccAddress([]byte("residual-attestor___"))

	gs := residualGenesis(
		types.StoreEntry{Key: types.AdminEpochKey("bank-a"), Value: epoch8(7)},
		types.StoreEntry{Key: types.HolderKycTierKey("bank-a", holder), Value: []byte{0, 0, 0, 2}},
		types.StoreEntry{Key: types.LastAttestorKey("bank-a"), Value: attestor.Bytes()},
		types.StoreEntry{Key: types.RedeemSubjectCounterKey(19_000, 'd', "did:phi:x"), Value: []byte("4200")},
	)
	require.NoError(t, gs.Validate())
}

// The consequence the admin-epoch check exists for: an epoch that survives the round trip keeps the approvals it retired retired.
func TestResidualValues_ARoundTrippedEpochKeepsRetiredApprovalsRetired(t *testing.T) {
	const retiredUnder = uint64(3)
	admin := sdk.AccAddress([]byte("residual-admin______"))

	gs := residualGenesis(types.StoreEntry{Key: types.AdminEpochKey("bank-a"), Value: epoch8(retiredUnder)})
	gs.Approvals = []types.StoreEntry{
		{Key: types.ApprovalKey("bank-a", []byte("content-hash"), admin), Value: epoch8(retiredUnder - 1)},
	}
	gs.Institutions = []types.Institution{{
		Id: "bank-a", Admin: admin.String(),
		InstitutionType: types.INSTITUTION_TYPE_FINANCIAL,
		VaultBalance:    "0", AttestedReserve: "0",
	}}
	require.NoError(t, gs.Validate())

	require.Equal(t, epoch8(retiredUnder), gs.StoreEntries[0].Value,
		"the epoch must round-trip exactly; reading as zero is what revives retired approvals")

	broken := gs
	broken.StoreEntries = []types.StoreEntry{{Key: types.AdminEpochKey("bank-a"), Value: []byte{3}}}
	require.Error(t, broken.Validate())
}
