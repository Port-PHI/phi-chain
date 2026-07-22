// SPDX-License-Identifier: Apache-2.0

package types

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func genesisWithRecoveryKeyType(keyType KeyType) GenesisState {
	did := "did:phi:1111111111111111111111111111111111111111111"
	ctrl := sdk.AccAddress([]byte("recovery-owner-addr1")).String()
	newCtrl := sdk.AccAddress([]byte("recovery-new-ctrl-01")).String()
	newKey := []byte("proposed-new-pub-key")
	nonce := []byte("genesis-nonce")

	return GenesisState{
		Params: DefaultParams(),
		Identities: []DIDDocument{{
			Did: did, Controller: ctrl, PubKey: []byte("pk"),
			UniquenessHash: []byte("uniq"), Status: DID_STATUS_ACTIVE, CreatedAt: 1,
		}},
		IdentityCount: 1,
		RecoveryRequests: []RecoveryRequest{{
			Did:                   did,
			ProposedNewController: newCtrl,
			ProposedNewPubKey:     newKey,
			Nonce:                 nonce,
			RecoveryId:            DeriveRecoveryID(did, newKey, nonce),
			KeyType:               keyType,
			Status:                RECOVERY_STATUS_PENDING,
			Method:                RECOVERY_METHOD_SOCIAL,
			InitiatedAt:           1,
			ExecuteAfter:          2,
			ExpiresAt:             3,
			DepositUphi:           "1000",
			FeeUphi:               "0",
		}},
	}
}

func TestGenesisValidate_RecoveryRequestKeyTypeMustBeExecutable(t *testing.T) {
	require.NoError(t, genesisWithRecoveryKeyType(KEY_TYPE_SECP256R1).Validate())

	for _, keyType := range []KeyType{KEY_TYPE_UNSPECIFIED, KEY_TYPE_SECP256K1} {
		t.Run(keyType.String(), func(t *testing.T) {
			err := genesisWithRecoveryKeyType(keyType).Validate()
			require.Error(t, err, "a request that could never execute must not import")
			require.Contains(t, err.Error(), "key_type")
		})
	}
}

// The zero value is the dangerous case: a genesis author who simply omits key_type gets KEY_TYPE_UNSPECIFIED, which previously imported silently.
func TestGenesisValidate_OmittedRecoveryKeyTypeIsRejected(t *testing.T) {
	gs := genesisWithRecoveryKeyType(KEY_TYPE_SECP256R1)
	gs.RecoveryRequests[0].KeyType = KeyType(0) // i.e. the field left unset

	require.Equal(t, KEY_TYPE_UNSPECIFIED, gs.RecoveryRequests[0].KeyType)
	require.Error(t, gs.Validate(), "an omitted key_type must not import as a silently dud request")
}
