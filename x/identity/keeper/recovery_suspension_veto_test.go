// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

// A freeze that begins while the veto window is still open must carry the unspent head-start forward: after a suspension that outlasts the original ExecuteAfter, execute stays refused until the preserved remainder elapses.
func TestRecoverySuspension_VetoWindowPausesAcrossAThaw(t *testing.T) {
	f := setupRecovery(t)
	base := f.now.Unix()
	id := f.initiate(t, "nonce-veto")
	f.approveToThreshold(t, id)

	execAfter := f.getReq(t, id).ExecuteAfter
	require.Greater(t, execAfter, base, "the veto window is in the future at initiation")

	freezeAt := base + 3600
	f.warpTo(freezeAt)
	f.suspend(t)
	require.Equal(t, freezeAt, f.getReq(t, id).FrozenAt, "a live request is stamped at the freeze instant")
	remaining := execAfter - freezeAt

	thawAt := execAfter + 10*3600
	f.warpTo(thawAt)
	f.reinstate(t)

	r := f.getReq(t, id)
	require.Zero(t, r.FrozenAt, "reinstatement clears the stamp")
	require.Equal(t, thawAt+remaining, r.ExecuteAfter,
		"the veto window resumes with exactly the time it had left when the freeze began")

	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.ErrorIs(t, err, types.ErrRecoveryTooEarly,
		"the veto must not collapse to a mempool race at reinstatement")

	f.warpTo(thawAt + remaining - 1)
	_, err = f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.ErrorIs(t, err, types.ErrRecoveryTooEarly)

	f.warpTo(thawAt + remaining)
	_, err = f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.NoError(t, err, "execute is permitted once the preserved veto window has fully elapsed")
}

// The control: a freeze that begins AFTER the veto window has already elapsed leaves ExecuteAfter untouched — there is no head-start left to preserve — so execute stays permissible right after thaw.
func TestRecoverySuspension_VetoAlreadyElapsedBeforeFreezeStaysPermissible(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, "nonce-veto-elapsed")
	f.approveToThreshold(t, id)

	execAfter := f.getReq(t, id).ExecuteAfter

	f.warpTo(execAfter + 3600)
	f.suspend(t)
	require.GreaterOrEqual(t, f.getReq(t, id).FrozenAt, execAfter,
		"the freeze began after the veto window had already closed")

	f.warpTo(execAfter + 2*3600)
	f.reinstate(t)
	require.Equal(t, execAfter, f.getReq(t, id).ExecuteAfter,
		"a veto that had already elapsed before the freeze is not pushed forward")

	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.NoError(t, err, "an already-elapsed veto leaves execute permissible right after reinstatement")
}
