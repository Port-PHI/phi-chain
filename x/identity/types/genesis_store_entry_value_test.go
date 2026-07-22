// SPDX-License-Identifier: Apache-2.0

package types_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

func epochValue(n byte) []byte { return []byte{0, 0, 0, 0, 0, 0, 0, n} }

func TestGenesis_RejectsAMalformedStoreEntryValue(t *testing.T) {
	valoper := sdk.ValAddress([]byte("validator-operator__")).String()

	for _, tc := range []struct {
		name  string
		entry types.StoreEntry
	}{
		{
			name: "short guardian epoch",
			entry: types.StoreEntry{
				Key: types.GuardianEpochKey("did:phi:owner"), Value: []byte{0x01},
			},
		},
		{
			name: "over-long guardian epoch",
			entry: types.StoreEntry{
				Key: types.GuardianEpochKey("did:phi:owner"), Value: append(epochValue(1), 0),
			},
		},
		{
			name: "short recovery tally epoch",
			entry: types.StoreEntry{
				Key: types.RecoveryTallyEpochKey(make([]byte, 32)), Value: []byte{0x02},
			},
		},
		{
			name: "empty issuer nonce marker",
			entry: types.StoreEntry{
				Key: types.IssuerNonceKey("did:phi:issuer", []byte("n")), Value: nil,
			},
		},
		{
			name: "empty recovery nonce marker",
			entry: types.StoreEntry{
				Key: types.RecoveryNonceKey("did:phi:owner", []byte("n")), Value: nil,
			},
		},
		{
			name: "did→validator value is not a validator address",
			entry: types.StoreEntry{
				Key: types.DIDToValidatorKey("did:phi:owner"), Value: []byte("not-a-valoper"),
			},
		},
		{
			name: "validator→did value is not a DID",
			entry: types.StoreEntry{
				Key: types.ValidatorToDIDKey(valoper), Value: []byte("not-a-did"),
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

// The counterpart: the entries the module itself produces must pass.
func TestGenesis_AcceptsWellFormedStoreEntryValues(t *testing.T) {
	valoper := sdk.ValAddress([]byte("validator-operator__")).String()

	gs := types.DefaultGenesis()
	gs.Identities = []types.DIDDocument{{
		Did: "did:phi:owner", Controller: sdk.AccAddress([]byte("owner-controller____")).String(),
		PubKey: []byte("pk-owner"), UniquenessHash: []byte("uniq-owner"),
		Status: types.DID_STATUS_ACTIVE,
	}}
	gs.IdentityCount = 1
	gs.StoreEntries = []types.StoreEntry{
		{Key: types.IssuerNonceKey("did:phi:issuer", []byte("burned")), Value: []byte{1}},
		{Key: types.RecoveryNonceKey("did:phi:owner", []byte("burned")), Value: []byte{1}},
		{Key: types.DIDToValidatorKey("did:phi:owner"), Value: []byte(valoper)},
		{Key: types.ValidatorToDIDKey(valoper), Value: []byte("did:phi:owner")},
		{Key: types.GuardianEpochKey("did:phi:owner"), Value: epochValue(4)},
		{Key: types.RecoveryTallyEpochKey(make([]byte, 32)), Value: epochValue(4)},
	}
	require.NoError(t, gs.Validate())
}
