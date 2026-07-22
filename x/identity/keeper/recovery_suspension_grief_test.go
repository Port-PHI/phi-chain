// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

func (f *recoveryFixture) suspend(t *testing.T) {
	t.Helper()
	_, err := f.msg.UpdateStatus(f.ctx, &types.MsgUpdateStatus{
		Authority: f.k.GetAuthority(), Did: f.did, NewStatus: types.DID_STATUS_SUSPENDED,
	})
	require.NoError(t, err)
}

func (f *recoveryFixture) touch(id []byte) error {
	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	return err
}

func (f *recoveryFixture) reinstate(t *testing.T) {
	t.Helper()
	_, err := f.msg.UpdateStatus(f.ctx, &types.MsgUpdateStatus{
		Authority: f.k.GetAuthority(), Did: f.did, NewStatus: types.DID_STATUS_ACTIVE,
	})
	require.NoError(t, err)
}

// TestRecoverySuspension_ASuspendedRecoveryIsNotExpiredOrForfeited is the grief itself: the whole TTL elapses while the DID is frozen, and the request must survive with its deposit intact.
func TestRecoverySuspension_ASuspendedRecoveryIsNotExpiredOrForfeited(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, "nonce-grief")
	f.approveToThreshold(t, id)

	escrowedBefore := f.escrowed()
	feesBefore := f.feeCollector()
	require.True(t, escrowedBefore.IsPositive(), "the deposit must actually be escrowed")

	f.suspend(t)
	f.warpPastTTL()

	require.Error(t, f.touch(id), "execution is refused while the DID is frozen")
	require.Equal(t, types.RECOVERY_STATUS_PENDING, f.status(t, id),
		"a request blocked purely by a suspension must not expire")
	require.Equal(t, escrowedBefore.String(), f.escrowed().String(),
		"the deposit must stay escrowed, not be forfeited to the fee collector")
	require.Equal(t, feesBefore.String(), f.feeCollector().String())
}

// And once the freeze lifts, the recovery completes and the deposit comes back to the initiator.
func TestRecoverySuspension_ARecoveryCompletesAfterTheSuspensionIsLifted(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, "nonce-resume")
	f.approveToThreshold(t, id)
	f.warpPastWindow()

	f.suspend(t)
	f.warpPastTTL()

	require.ErrorIs(t, f.touch(id), types.ErrInvalidRecovery,
		"a suspended DID still cannot be recovered through")
	require.Equal(t, types.RECOVERY_STATUS_PENDING, f.status(t, id))

	f.reinstate(t)

	balanceBefore := f.newCtrlBalance(t)
	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.NoError(t, err, "the recovery must still be completable once the freeze lifts")
	require.True(t, f.newCtrlBalance(t).GT(balanceBefore),
		"the deposit must be refunded to the initiator, not forfeited to the griefer's window")
}

// The window really is restored rather than merely deferred: after reinstatement the initiator has a full time-to-live again, not the remnant of one the freeze already consumed.
func TestRecoverySuspension_TheWindowRestartsOnReinstatement(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, "nonce-window")
	f.approveToThreshold(t, id)
	f.warpPastWindow()

	f.suspend(t)
	f.warpPastTTL()
	require.Error(t, f.touch(id))
	f.reinstate(t)

	r, found := f.k.GetRecoveryRequest(f.ctx, id)
	require.True(t, found)
	require.Greater(t, r.ExpiresAt, f.ctx.BlockTime().Unix(),
		"the request must carry a live deadline after the freeze, not an already-elapsed one")

	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.NoError(t, err)
}

// REVOCATION IS UNTOUCHED.
func TestRecoverySuspension_RevocationStillTerminatesRecovery(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, "nonce-revoked")
	f.approveToThreshold(t, id)

	feesBefore := f.feeCollector()

	_, err := f.msg.RevokeIdentity(f.ctx, &types.MsgRevokeIdentity{Creator: f.oldCtrl, Did: f.did})
	require.NoError(t, err)

	f.warpPastTTL()
	_ = f.touch(id)
	_, stillOpen := f.k.GetRecoveryRequest(f.ctx, id)
	require.False(t, stillOpen, "a revoked DID's recovery must not be held open")
	require.True(t, f.feeCollector().GT(feesBefore),
		"a revoked DID's deposit is still forfeited; revocation is terminal, not a freeze")
}

// An ordinary abandonment still expires and still forfeits: the hold is for a request the chain PREVENTED from proceeding, not for one nobody pursued.
func TestRecoverySuspension_AnUnsuspendedRequestStillExpires(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, "nonce-abandoned")

	feesBefore := f.feeCollector()
	f.warpPastTTL()
	_ = f.touch(id)

	_, stillOpen := f.k.GetRecoveryRequest(f.ctx, id)
	require.False(t, stillOpen, "an abandoned request must still expire")
	require.True(t, f.feeCollector().GT(feesBefore),
		"expiry must still forfeit when nothing prevented the initiator from acting")
}
