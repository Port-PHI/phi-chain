// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

type recoveryStatusCase struct {
	status     types.DIDStatus
	reachable  bool
	executable bool
}

func recoveryStatusCases() []recoveryStatusCase {
	return []recoveryStatusCase{
		{status: types.DID_STATUS_ACTIVE, reachable: true, executable: true},
		{status: types.DID_STATUS_SUSPENDED, reachable: true, executable: false},
		{status: types.DID_STATUS_REVOKED, reachable: false, executable: false},
	}
}

func (f *recoveryFixture) setStatus(t *testing.T, status types.DIDStatus) {
	t.Helper()
	switch status {
	case types.DID_STATUS_ACTIVE:
	case types.DID_STATUS_SUSPENDED:
		_, err := f.msg.UpdateStatus(f.ctx, &types.MsgUpdateStatus{
			Authority: f.k.GetAuthority(), Did: f.did, NewStatus: types.DID_STATUS_SUSPENDED,
		})
		require.NoError(t, err)
	case types.DID_STATUS_REVOKED:
		_, err := f.msg.RevokeIdentity(f.ctx, &types.MsgRevokeIdentity{Creator: f.oldCtrl, Did: f.did})
		require.NoError(t, err)
	}
}

// TestRecovery_StatusGateAndOwnerVetoAcrossEveryStatus covers every DID status and asserts the pairing that actually matters, rather than either half of it alone: in no status may a recovery be EXECUTABLE while the owner's veto is UNREACHABLE.
func TestRecovery_StatusGateAndOwnerVetoAcrossEveryStatus(t *testing.T) {
	for _, tc := range recoveryStatusCases() {
		t.Run(tc.status.String(), func(t *testing.T) {
			exec := setupRecovery(t)
			id := exec.initiate(t, recoveryNonce)
			exec.approveToThreshold(t, id)
			exec.warpPastWindow()
			exec.setStatus(t, tc.status)

			_, stillOpen := exec.k.GetRecoveryRequest(exec.ctx, id)
			require.Equal(t, tc.reachable, stillOpen,
				"a request must survive into this status exactly when recovery is reachable there")

			_, execErr := exec.msg.ExecuteRecovery(exec.ctx, &types.MsgExecuteRecovery{
				Creator: exec.newCtrl, RecoveryId: id,
			})
			if tc.executable {
				require.NoError(t, execErr, "recovery must execute on an ACTIVE DID")
			} else {
				require.Error(t, execErr, "recovery must not execute on a %s DID", tc.status)
			}

			doc, found := exec.k.GetIdentity(exec.ctx, exec.did)
			require.True(t, found)
			if tc.executable {
				require.Equal(t, exec.newCtrl, doc.Controller)
			} else {
				require.Equal(t, exec.oldCtrl, doc.Controller,
					"a blocked recovery must not move the controller")
			}

			if !tc.reachable {
				require.False(t, tc.executable, "an unreachable recovery cannot be executable")
				return
			}
			veto := setupRecovery(t)
			vetoID := veto.initiate(t, recoveryNonce)
			veto.setStatus(t, tc.status)

			ownerCanTransact := !veto.k.HasNonActiveDID(veto.ctx, veto.oldCtrl)
			_, cancelErr := veto.msg.CancelRecovery(veto.ctx, &types.MsgCancelRecovery{
				Creator: veto.oldCtrl, RecoveryId: vetoID,
			})
			vetoReachable := ownerCanTransact && cancelErr == nil

			if tc.executable {
				require.True(t, vetoReachable,
					"a recovery that can execute in status %s must be one its owner can still veto", tc.status)
				veto.requireSettled(t, vetoID)
			} else {
				require.False(t, ownerCanTransact,
					"only a status that silences the owner may also be one that blocks execution")
			}
		})
	}
}

// TestRecovery_SuspensionCannotBeUsedToPushARecoveryThrough is the concrete attack: the DID is suspended after a recovery is approved and its window has elapsed.
func TestRecovery_SuspensionCannotBeUsedToPushARecoveryThrough(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)
	f.approveToThreshold(t, id)
	f.warpPastWindow()

	f.setStatus(t, types.DID_STATUS_SUSPENDED)
	require.True(t, f.k.HasNonActiveDID(f.ctx, f.oldCtrl), "a suspended owner cannot send any transaction")

	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.ErrorIs(t, err, types.ErrInvalidRecovery)
	doc, _ := f.k.GetIdentity(f.ctx, f.did)
	require.Equal(t, f.oldCtrl, doc.Controller, "the identity must not have changed hands")

	_, err = f.msg.UpdateStatus(f.ctx, &types.MsgUpdateStatus{
		Authority: f.k.GetAuthority(), Did: f.did, NewStatus: types.DID_STATUS_ACTIVE,
	})
	require.NoError(t, err)
	require.False(t, f.k.HasNonActiveDID(f.ctx, f.oldCtrl))

	_, err = f.msg.CancelRecovery(f.ctx, &types.MsgCancelRecovery{Creator: f.oldCtrl, RecoveryId: id})
	require.NoError(t, err)
	f.requireSettled(t, id)
}
