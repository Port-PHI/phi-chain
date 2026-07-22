// SPDX-License-Identifier: Apache-2.0

package types

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func genesisWithRecovery(t *testing.T, rid []byte) GenesisState {
	t.Helper()
	ctrl := sdk.AccAddress([]byte("ctrl________________")).String()
	newCtrl := sdk.AccAddress([]byte("new-controller______")).String()
	did := realDID(t, "recovery-key")
	active := DIDDocument{
		Did: did, Controller: ctrl, PubKey: []byte("pk"),
		UniquenessHash: []byte("uniq"), Status: DID_STATUS_ACTIVE, CreatedAt: 1,
	}
	req := RecoveryRequest{
		Did:                   did,
		ProposedNewController: newCtrl,
		ProposedNewPubKey:     []byte("new-pub-key"),
		Nonce:                 []byte("nonce-abc"),
		RecoveryId:            rid,
		KeyType:               KEY_TYPE_SECP256R1, // the only curve a recovery can install
		Status:                RECOVERY_STATUS_PENDING,
		Method:                RECOVERY_METHOD_SOCIAL,
		InitiatedAt:           1,
		ExecuteAfter:          2,
		ExpiresAt:             3,
		DepositUphi:           "1000",
		FeeUphi:               "10",
	}
	return GenesisState{
		Params:           DefaultParams(),
		Identities:       []DIDDocument{active},
		IdentityCount:    1,
		RecoveryRequests: []RecoveryRequest{req},
	}
}

// TestGenesisValidate_MalformedRecoveryID pins that a short/malformed recovery_id fails validation with a clean error rather than panicking.
func TestGenesisValidate_MalformedRecoveryID(t *testing.T) {
	good := genesisWithRecovery(t, nil)
	good.RecoveryRequests[0].RecoveryId = DeriveRecoveryID(
		good.RecoveryRequests[0].Did,
		good.RecoveryRequests[0].ProposedNewPubKey,
		good.RecoveryRequests[0].Nonce,
	)
	require.NoError(t, good.Validate(), "a canonical recovery_id must validate")

	short := genesisWithRecovery(t, []byte{0x01, 0x02, 0x03})
	require.NotPanics(t, func() {
		require.Error(t, short.Validate(), "a too-short recovery_id must fail validation")
	}, "genesis validation must not panic on a malformed recovery_id")

	empty := genesisWithRecovery(t, []byte{})
	require.NotPanics(t, func() {
		require.Error(t, empty.Validate())
	})
}
