// SPDX-License-Identifier: Apache-2.0

package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

// A RecoveryRequest carrying rejections survives a marshal→unmarshal unchanged, and Size() agrees with the bytes actually produced.
func TestRecoveryRequest_RejectionsWireRoundTrip(t *testing.T) {
	in := types.RecoveryRequest{
		RecoveryId:            []byte{0x01, 0x02, 0x03},
		Did:                   "did:phi:abc",
		ProposedNewPubKey:     []byte{0x09, 0x00, 0x0a}, // embedded NUL: the framing must not care
		ProposedNewController: "phi1controller",
		KeyType:               types.KEY_TYPE_SECP256R1,
		Method:                types.RECOVERY_METHOD_SOCIAL,
		Approvals:             []string{"did:phi:g1", "did:phi:g2"},
		Nonce:                 []byte("nonce"),
		InitiatedAt:           1_000_000,
		ExecuteAfter:          1_259_200,
		ExpiresAt:             2_209_600,
		DepositUphi:           "1000000",
		Status:                types.RECOVERY_STATUS_PENDING,
		FeeUphi:               "0",
		Rejections:            []string{"did:phi:r1", "did:phi:r2", "did:phi:r3"},
	}

	bz, err := in.Marshal()
	require.NoError(t, err)
	require.Equal(t, in.Size(), len(bz), "Size() must equal the encoded length")

	var out types.RecoveryRequest
	require.NoError(t, out.Unmarshal(bz))
	require.Equal(t, in, out, "the whole message must survive the round trip")
	require.Equal(t, in.Rejections, out.Rejections)
	require.Equal(t, in.Approvals, out.Approvals, "rejections must not disturb the neighbouring field")
}

// The field is genuinely optional on the wire: a request with no rejections encodes exactly as it did before the field existed, so old state decodes unchanged and the addition is backward compatible.
func TestRecoveryRequest_NoRejectionsAddsNoBytes(t *testing.T) {
	base := types.RecoveryRequest{
		RecoveryId: []byte{0x01}, Did: "did:phi:abc", Method: types.RECOVERY_METHOD_SOCIAL,
		Status: types.RECOVERY_STATUS_PENDING, DepositUphi: "1000000", FeeUphi: "0",
	}
	withEmpty := base
	withEmpty.Rejections = []string{}

	a, err := base.Marshal()
	require.NoError(t, err)
	b, err := withEmpty.Marshal()
	require.NoError(t, err)
	require.Equal(t, a, b, "an empty rejection list must occupy no bytes")

	var out types.RecoveryRequest
	require.NoError(t, out.Unmarshal(a))
	require.Empty(t, out.Rejections)
}

// MsgRejectRecovery round-trips, and carries the same shape as the approval message it mirrors.
func TestMsgRejectRecovery_WireRoundTrip(t *testing.T) {
	in := types.MsgRejectRecovery{
		Creator:     "phi1guardiancontroller",
		RecoveryId:  []byte{0xde, 0xad, 0x00, 0xbe, 0xef},
		GuardianDid: "did:phi:guardian",
		Salt:        make([]byte, types.GuardianSaltLen),
	}
	copy(in.Salt, "salt-guardian")

	bz, err := in.Marshal()
	require.NoError(t, err)
	require.Equal(t, in.Size(), len(bz))

	var out types.MsgRejectRecovery
	require.NoError(t, out.Unmarshal(bz))
	require.Equal(t, in, out)

	approve := types.MsgApproveRecovery{
		Creator: in.Creator, RecoveryId: in.RecoveryId, GuardianDid: in.GuardianDid, Salt: in.Salt,
	}
	abz, err := approve.Marshal()
	require.NoError(t, err)
	require.Equal(t, abz, bz)
}

// The accessors the generated code is expected to expose.
func TestMsgRejectRecovery_Getters(t *testing.T) {
	m := types.MsgRejectRecovery{
		Creator: "phi1x", RecoveryId: []byte{1}, GuardianDid: "did:phi:g", Salt: []byte{2},
	}
	require.Equal(t, "phi1x", m.GetCreator())
	require.Equal(t, []byte{1}, m.GetRecoveryId())
	require.Equal(t, "did:phi:g", m.GetGuardianDid())
	require.Equal(t, []byte{2}, m.GetSalt())

	var nilReq *types.RecoveryRequest
	require.Nil(t, nilReq.GetRejections(), "the getter must be nil-safe, as generated getters are")
}
