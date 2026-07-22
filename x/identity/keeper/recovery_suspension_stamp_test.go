// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

func (f *recoveryFixture) getReq(t *testing.T, id []byte) types.RecoveryRequest {
	t.Helper()
	r, found := f.k.GetRecoveryRequest(f.ctx, id)
	require.True(t, found, "request must still exist")
	return r
}

func (f *recoveryFixture) warpTo(sec int64) { f.ctx = f.ctx.WithBlockTime(time.Unix(sec, 0)) }

func (f *recoveryFixture) ttl(t *testing.T) int64 {
	t.Helper()
	return int64(f.k.GetParams(f.ctx).RecoveryRequestTtlSeconds)
}

// TestRecoverySuspension_AnExpiredRequestIsNotRevivedByALaterSuspension is the takeover path: a request whose TTL has already elapsed must NOT be resurrected by a suspension that arrives afterwards.
func TestRecoverySuspension_AnExpiredRequestIsNotRevivedByALaterSuspension(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, "nonce-h1")
	f.approveToThreshold(t, id)

	feesBefore := f.feeCollector()

	f.warpPastTTL()

	f.suspend(t)
	require.Zero(t, f.getReq(t, id).FrozenAt,
		"a request already past its TTL must carry no suspension-hold stamp")

	require.Error(t, f.touch(id), "an expired request cannot be executed, freeze or no freeze")
	_, stillOpen := f.k.GetRecoveryRequest(f.ctx, id)
	require.False(t, stillOpen, "the expired request must be reaped, not held open by the later freeze")
	require.True(t, f.feeCollector().GT(feesBefore),
		"the deposit is forfeited exactly as an ordinary expiry forfeits it")

	f.reinstate(t)
	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.Error(t, err, "a reaped request cannot be executed after reinstatement — no takeover")
}

// TestRecoverySuspension_AGenuinelyLiveRequestCompletesAfterAFreeze is the other half of the comprehensive requirement: a request that WAS live when the freeze began is paused, survives past what would have been its deadline, and completes once the DID is reinstated.
func TestRecoverySuspension_AGenuinelyLiveRequestCompletesAfterAFreeze(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, "nonce-live")
	f.approveToThreshold(t, id)
	f.warpPastWindow() // the opposition window elapses; the request is live and would be executable

	f.suspend(t)
	require.NotZero(t, f.getReq(t, id).FrozenAt, "a live request must be stamped when the freeze begins")

	f.warpPastTTL()
	require.Error(t, f.touch(id), "a suspended DID still cannot be recovered through")
	require.Equal(t, types.RECOVERY_STATUS_PENDING, f.status(t, id), "the hold keeps it alive")

	f.reinstate(t)
	require.Zero(t, f.getReq(t, id).FrozenAt, "reinstatement clears the stamp")

	balanceBefore := f.newCtrlBalance(t)
	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.NoError(t, err, "a genuinely-paused recovery completes on reinstatement")
	require.True(t, f.newCtrlBalance(t).GT(balanceBefore), "and its deposit is refunded, not forfeited")
}

// A suspension landing EXACTLY on the expiry boundary protects the request: at that instant it is still live (expiry is `now > ExpiresAt`, strict), so the stamp is taken and the two boundary tests agree.
func TestRecoverySuspension_FreezeExactlyOnTheExpiryBoundaryProtects(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, "nonce-boundary")
	f.approveToThreshold(t, id)

	exp := f.getReq(t, id).ExpiresAt
	f.warpTo(exp) // now == ExpiresAt: the last instant the request is still live
	f.suspend(t)
	require.Equal(t, exp, f.getReq(t, id).FrozenAt, "a request live at the boundary is stamped")

	f.warpPastTTL()
	require.Error(t, f.touch(id))
	require.Equal(t, types.RECOVERY_STATUS_PENDING, f.status(t, id), "held, not expired")

	f.reinstate(t)
	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.NoError(t, err)
}

// A freeze arriving ONE SECOND after the boundary does NOT protect: the request had already expired, so no stamp is taken and it is reaped on the next touch.
func TestRecoverySuspension_FreezeOneSecondPastTheBoundaryDoesNotProtect(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, "nonce-past")
	f.approveToThreshold(t, id)

	feesBefore := f.feeCollector()
	exp := f.getReq(t, id).ExpiresAt
	f.warpTo(exp + 1) // one second past: already expired
	f.suspend(t)
	require.Zero(t, f.getReq(t, id).FrozenAt, "a request expired one second before the freeze is not stamped")

	require.Error(t, f.touch(id))
	_, stillOpen := f.k.GetRecoveryRequest(f.ctx, id)
	require.False(t, stillOpen, "reaped, not held")
	require.True(t, f.feeCollector().GT(feesBefore))
}

// Multiple suspend/reinstate cycles: each freeze re-stamps a still-live request and each reinstatement hands back a full window and clears the stamp.
func TestRecoverySuspension_MultipleCyclesRestampAndRestore(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, "nonce-cycles")
	f.approveToThreshold(t, id)
	f.warpPastWindow()

	base := f.now.Add(73 * time.Hour).Unix()
	for cycle := range int64(3) {
		f.warpTo(base + cycle*100)
		f.suspend(t)
		require.NotZero(t, f.getReq(t, id).FrozenAt, "cycle %d: a live request is re-stamped on freeze", cycle)

		f.warpTo(base + cycle*100 + 50)
		f.reinstate(t)
		r := f.getReq(t, id)
		require.Zero(t, r.FrozenAt, "cycle %d: reinstatement clears the stamp", cycle)
		require.Equal(t, f.ctx.BlockTime().Unix()+f.ttl(t), r.ExpiresAt,
			"cycle %d: reinstatement restores a full window", cycle)
	}

	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.NoError(t, err, "the request survives repeated cycles and still completes")
}

// The stamp must never let a TRULY-ABANDONED request survive: once a DID is reinstated the stamp is gone, so a request nobody pursues afterwards expires and forfeits on schedule, exactly like one that was never frozen at all.
func TestRecoverySuspension_ThawedThenAbandonedStillExpires(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, "nonce-thaw-abandon")

	f.suspend(t)
	require.NotZero(t, f.getReq(t, id).FrozenAt)
	f.reinstate(t)
	require.Zero(t, f.getReq(t, id).FrozenAt)

	feesBefore := f.feeCollector()
	f.warpTo(f.getReq(t, id).ExpiresAt + 1)
	require.Error(t, f.touch(id))
	_, stillOpen := f.k.GetRecoveryRequest(f.ctx, id)
	require.False(t, stillOpen, "an abandoned request after a thaw must still expire")
	require.True(t, f.feeCollector().GT(feesBefore), "and still forfeit — the stamp granted no permanent hold")
}
