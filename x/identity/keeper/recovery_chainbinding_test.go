// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"bytes"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/phicrypto"
	"github.com/Port-PHI/phi-chain/x/identity/types"
)

func capturingVerifier(captured *[]byte) phicrypto.Verifier {
	popPrefix := types.CanonicalMessage(types.SocialRecoveryPoPDomain)
	f := phicrypto.AcceptAll()
	f.SignatureFn = func(_ phicrypto.Curve, _, msg, _ []byte) bool {
		if bytes.HasPrefix(msg, popPrefix) {
			*captured = append([]byte(nil), msg...)
		}
		return true
	}
	return f
}

func TestRecovery_SocialPoPBindsTheChainID(t *testing.T) {
	var popMsg []byte
	ctx, k, msg, bank := setupIdentityFull(t, capturingVerifier(&popMsg))
	now := time.Unix(1_000_000, 0)
	ctx = ctx.WithBlockTime(now)

	oldCtrl := someAddr("chainbind-owner_____")
	did := registerActive(t, ctx, msg, oldCtrl, "chainbind-owner", []byte("bio-chainbind"))

	guardians, commitments := guardianPool(t, ctx, msg, 3)
	require.Len(t, guardians, 3)
	_, err := msg.SetGuardians(ctx, &types.MsgSetGuardians{
		Controller: oldCtrl, Did: did, Commitments: commitments, Threshold: 2,
	})
	require.NoError(t, err)

	newCtrl := someAddr("chainbind-newctrl___")
	addr, err := sdk.AccAddressFromBech32(newCtrl)
	require.NoError(t, err)
	bank.Fund(addr, k.GetParams(ctx).RecoveryDeposit().MulRaw(10))

	newKey := pubFor("chainbind-new-key")
	nonce := []byte("chainbind-nonce")
	_, err = msg.InitiateRecovery(ctx, &types.MsgInitiateRecovery{
		Creator:           newCtrl,
		Did:               did,
		ProposedNewPubKey: newKey,
		KeyType:           types.KEY_TYPE_SECP256R1,
		Method:            types.RECOVERY_METHOD_SOCIAL,
		Nonce:             nonce,
		PopSig:            []byte("pop"),
	})
	require.NoError(t, err)
	require.NotEmpty(t, popMsg, "the keeper must have checked a social-recovery proof-of-possession")

	require.Equal(t,
		types.SocialRecoveryPoPMessage(ctx.ChainID(), did, newKey, newCtrl, nonce),
		popMsg,
		"the proof-of-possession must cover this chain's id")

	require.NotEqual(t,
		types.SocialRecoveryPoPMessage("phi-some-other-chain", did, newKey, newCtrl, nonce),
		popMsg,
		"an assertion made for another network must not be the message this chain checks")
}
