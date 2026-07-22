// SPDX-License-Identifier: Apache-2.0

package types_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

func validReject() *types.MsgRejectRecovery {
	return &types.MsgRejectRecovery{
		Creator:     sdk.AccAddress([]byte("guardian-controller1")).String(),
		RecoveryId:  make([]byte, types.RecoveryIDLen),
		GuardianDid: "did:phi:2222222222222222222222222222222222222222222",
		Salt:        make([]byte, types.GuardianSaltLen),
	}
}

// The message must actually be routed through stateless validation at all.
func TestMsgRejectRecovery_ImplementsValidation(t *testing.T) {
	var msg sdk.Msg = &types.MsgRejectRecovery{}
	_, ok := msg.(interface{ ValidateBasic() error })
	require.True(t, ok, "a rejection with no ValidateBasic would silently skip stateless validation")
}

func TestMsgRejectRecovery_AcceptsAWellFormedMessage(t *testing.T) {
	require.NoError(t, validReject().ValidateBasic())
}

func TestMsgRejectRecovery_RejectsMalformedMessages(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*types.MsgRejectRecovery)
	}{
		{"creator is not an address", func(m *types.MsgRejectRecovery) { m.Creator = "not-bech32" }},
		{"creator is empty", func(m *types.MsgRejectRecovery) { m.Creator = "" }},
		{"recovery id is empty", func(m *types.MsgRejectRecovery) { m.RecoveryId = nil }},
		{"recovery id is short", func(m *types.MsgRejectRecovery) { m.RecoveryId = make([]byte, types.RecoveryIDLen-1) }},
		{"recovery id is long", func(m *types.MsgRejectRecovery) { m.RecoveryId = make([]byte, types.RecoveryIDLen+1) }},
		{"guardian did is empty", func(m *types.MsgRejectRecovery) { m.GuardianDid = "" }},
		{"guardian did is malformed", func(m *types.MsgRejectRecovery) { m.GuardianDid = "not-a-did" }},
		{"salt is empty", func(m *types.MsgRejectRecovery) { m.Salt = nil }},
		{"salt is short", func(m *types.MsgRejectRecovery) { m.Salt = make([]byte, types.GuardianSaltLen-1) }},
		{"salt is oversized", func(m *types.MsgRejectRecovery) { m.Salt = make([]byte, 1<<20) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := validReject()
			tc.mutate(m)
			require.Error(t, m.ValidateBasic())
		})
	}
}

// Approving and rejecting must accept exactly the same shapes: if one were laxer, the cheaper forgery would be the one an attacker reaches for.
func TestMsgRejectRecovery_ValidatesAsStrictlyAsApproval(t *testing.T) {
	mutations := []func(reject *types.MsgRejectRecovery, approve *types.MsgApproveRecovery){
		func(r *types.MsgRejectRecovery, a *types.MsgApproveRecovery) { r.Creator, a.Creator = "bad", "bad" },
		func(r *types.MsgRejectRecovery, a *types.MsgApproveRecovery) { r.RecoveryId, a.RecoveryId = nil, nil },
		func(r *types.MsgRejectRecovery, a *types.MsgApproveRecovery) { r.GuardianDid, a.GuardianDid = "", "" },
		func(r *types.MsgRejectRecovery, a *types.MsgApproveRecovery) { r.Salt, a.Salt = []byte{1}, []byte{1} },
	}

	for i, mutate := range mutations {
		reject := validReject()
		approve := &types.MsgApproveRecovery{
			Creator: reject.Creator, RecoveryId: reject.RecoveryId,
			GuardianDid: reject.GuardianDid, Salt: reject.Salt,
		}
		require.NoError(t, reject.ValidateBasic(), "case %d baseline", i)
		require.NoError(t, approve.ValidateBasic(), "case %d baseline", i)

		mutate(reject, approve)
		require.Equal(t, approve.ValidateBasic() != nil, reject.ValidateBasic() != nil,
			"case %d: rejection and approval must agree on what is well-formed", i)
	}
}
