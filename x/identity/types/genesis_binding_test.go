// SPDX-License-Identifier: Apache-2.0

package types

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// TestGenesisValidate_ValidatorDIDBinding covers the case where the validator↔DID genesis binding must be
// a bijection to an existing, ACTIVE DID.
func TestGenesisValidate_ValidatorDIDBinding(t *testing.T) {
	ctrl := sdk.AccAddress([]byte("ctrl________________")).String()
	did := DeriveDIDFromP256([]byte("validator-key"))
	valoper := "phivaloper1examplexyz"
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

	// Missing reverse mapping.
	require.Error(t, gen([]DIDDocument{active}, []StoreEntry{{Key: DIDToValidatorKey(did), Value: []byte(valoper)}}).Validate())

	// Mismatched reverse mapping.
	require.Error(t, gen([]DIDDocument{active}, []StoreEntry{
		{Key: DIDToValidatorKey(did), Value: []byte(valoper)},
		{Key: ValidatorToDIDKey(valoper), Value: []byte(DeriveDIDFromP256([]byte("other")))},
	}).Validate())

	// Binding to a DID absent from the identity set.
	require.Error(t, gen([]DIDDocument{}, bind(did, valoper)).Validate())

	// Binding to a REVOKED DID.
	revoked := active
	revoked.Status = DID_STATUS_REVOKED
	require.Error(t, gen([]DIDDocument{revoked}, bind(did, valoper)).Validate())
}
