// SPDX-License-Identifier: Apache-2.0

package ante_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256r1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/stretchr/testify/require"

	identitytypes "github.com/Port-PHI/phi-chain/x/identity/types"
)

// Rejecting a recovery is deposit-affecting and terminal, so it must demand a UV gesture, not a bare presence tap.
func TestRouter_SteppedUV_RejectRecoveryRequiresUV(t *testing.T) {
	rejectMsg := func(acc sdk.AccountI) sdk.Msg {
		return &identitytypes.MsgRejectRecovery{
			Creator:     acc.GetAddress().String(),
			RecoveryId:  make([]byte, identitytypes.RecoveryIDLen),
			GuardianDid: "did:phi:guardian",
			Salt:        make([]byte, identitytypes.GuardianSaltLen),
		}
	}

	policy := fakeUVPolicy{
		sensitive:     identitytypes.DefaultUVSensitiveMsgTypeURLs,
		largeTransfer: sensitiveUVPolicy().largeTransfer,
	}

	for _, tc := range []struct {
		name      string
		uv        bool
		wantError bool
	}{
		{"reject without a verification gesture is refused", false, true},
		{"reject with a verification gesture is accepted", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAnteFixture(t)
			r1, err := secp256r1.GenPrivKey()
			require.NoError(t, err)
			acc := f.mkAccount(t, r1.PubKey(), 0)

			tx, _ := f.envelopeTxMsgs(t, r1.PubKey(), acc, uvEnvelope(tc.uv),
				signing.SignMode_SIGN_MODE_DIRECT, rejectMsg(acc))

			dec := f.phiDecoratorUV(uvAwareVerifier(), uvWebAuthnParams(), policy)
			_, err = f.run(dec, tx)
			if tc.wantError {
				require.Error(t, err, "a recovery rejection without a User-Verification gesture must be refused")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// MsgRejectRecovery is in the shipped default UV-sensitive set.
func TestRouter_SteppedUV_RejectRecoveryIsInTheShippedDefault(t *testing.T) {
	want := sdk.MsgTypeURL(&identitytypes.MsgRejectRecovery{})
	require.Contains(t, identitytypes.DefaultUVSensitiveMsgTypeURLs, want,
		"rejecting a recovery must be user-verification sensitive out of the box")

	require.Contains(t, identitytypes.DefaultUVSensitiveMsgTypeURLs,
		sdk.MsgTypeURL(&identitytypes.MsgApproveRecovery{}))
}
