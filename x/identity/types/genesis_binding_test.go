// SPDX-License-Identifier: Apache-2.0

package types

import (
	"crypto/elliptic"
	"crypto/sha256"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func realDID(t *testing.T, label string) string {
	t.Helper()
	scalar := sha256.Sum256([]byte("phi-test-p256-" + label))
	x, y := elliptic.P256().ScalarBaseMult(scalar[:])
	did, err := DeriveDIDFromP256(elliptic.Marshal(elliptic.P256(), x, y))
	require.NoError(t, err)
	return did
}

// TestGenesisValidate_ValidatorDIDBinding covers the case where the validator↔DID genesis binding must be a bijection to an existing, ACTIVE DID.
func TestGenesisValidate_ValidatorDIDBinding(t *testing.T) {
	ctrl := sdk.AccAddress([]byte("ctrl________________")).String()
	did := realDID(t, "validator-key")
	valoper := sdk.ValAddress([]byte("validator-operator__")).String()
	active := DIDDocument{Did: did, Controller: ctrl, PubKey: []byte("pk"), UniquenessHash: []byte("uniq"), Status: DID_STATUS_ACTIVE, CreatedAt: 1}

	gen := func(ids []DIDDocument, entries []StoreEntry) GenesisState {
		return GenesisState{Params: DefaultParams(), Identities: ids, IdentityCount: uint64(len(ids)), StoreEntries: entries}
	}
	bind := func(d, v string) []StoreEntry {
		return []StoreEntry{
			{Key: DIDToValidatorKey(d), Value: []byte(v)},
			{Key: ValidatorToDIDKey(v), Value: []byte(d)},
		}
	}

	require.NoError(t, gen([]DIDDocument{active}, bind(did, valoper)).Validate(),
		"a bijective binding to an ACTIVE DID must validate")

	require.Error(t, gen([]DIDDocument{active}, []StoreEntry{{Key: DIDToValidatorKey(did), Value: []byte(valoper)}}).Validate())

	require.Error(t, gen([]DIDDocument{active}, []StoreEntry{
		{Key: DIDToValidatorKey(did), Value: []byte(valoper)},
		{Key: ValidatorToDIDKey(valoper), Value: []byte(realDID(t, "other"))},
	}).Validate())

	require.Error(t, gen([]DIDDocument{}, bind(did, valoper)).Validate())

	revoked := active
	revoked.Status = DID_STATUS_REVOKED
	require.Error(t, gen([]DIDDocument{revoked}, bind(did, valoper)).Validate())
}

// Genesis must accept exactly the bindings the runtime can produce, for EVERY status, so a chain cannot be launched into a state the sweep would immediately have to correct — nor refuse a state the sweep deliberately maintains.
func TestGenesisValidate_ValidatorBindingAcceptsExactlyTheLiveStatuses(t *testing.T) {
	ctrl := sdk.AccAddress([]byte("ctrl________________")).String()
	did := realDID(t, "validator-key")
	valoper := sdk.ValAddress([]byte("validator-operator__")).String()

	for _, tc := range []struct {
		name    string
		status  DIDStatus
		wantErr bool
	}{
		{name: "ACTIVE", status: DID_STATUS_ACTIVE, wantErr: false},
		{name: "SUSPENDED", status: DID_STATUS_SUSPENDED, wantErr: false},
		{name: "REVOKED", status: DID_STATUS_REVOKED, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := DIDDocument{
				Did: did, Controller: ctrl, PubKey: []byte("pk"),
				UniquenessHash: []byte("uniq"), Status: tc.status, CreatedAt: 1,
			}
			gs := GenesisState{
				Params: DefaultParams(), Identities: []DIDDocument{doc}, IdentityCount: 1,
				StoreEntries: []StoreEntry{
					{Key: DIDToValidatorKey(did), Value: []byte(valoper)},
					{Key: ValidatorToDIDKey(valoper), Value: []byte(did)},
				},
			}
			if tc.wantErr {
				require.Error(t, gs.Validate(),
					"a validator bound to a %s DID must not be importable", tc.name)
				return
			}
			require.NoError(t, gs.Validate(),
				"a validator bound to a %s DID is a state the runtime maintains and must import", tc.name)
		})
	}
}
