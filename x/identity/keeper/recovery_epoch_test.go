// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

func (f *recoveryFixture) rotateGuardians(t *testing.T, label string, threshold uint32) []string {
	t.Helper()
	guardians, commitments := guardianPoolLabelled(t, f.ctx, f.msg, 5, label)
	_, err := f.msg.SetGuardians(f.ctx, &types.MsgSetGuardians{
		Controller: f.oldCtrl, Did: f.did, Commitments: commitments, Threshold: threshold,
	})
	require.NoError(t, err)
	f.guardians = guardians
	return guardians
}

func (f *recoveryFixture) approveAsLabelled(t *testing.T, id []byte, label string, i int) error {
	t.Helper()
	_, err := f.msg.ApproveRecovery(f.ctx, &types.MsgApproveRecovery{
		Creator:     guardianCtrlLabelled(label, i),
		RecoveryId:  id,
		GuardianDid: f.guardians[i],
		Salt:        saltFor(fmt.Sprintf("%s-%d", label, i)),
	})
	return err
}

// THE PIN.
func TestRecovery_ApprovalsFromASupersededGuardianSetDoNotCount(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)

	f.approveToThreshold(t, id)
	req, found := f.k.GetRecoveryRequest(f.ctx, id)
	require.True(t, found)
	require.Len(t, f.k.EffectiveApprovals(f.ctx, req), 3, "the original tally counts while its set is in force")

	f.rotateGuardians(t, "fresh-guardian", 3)

	req, found = f.k.GetRecoveryRequest(f.ctx, id)
	require.True(t, found)
	require.Empty(t, f.k.EffectiveApprovals(f.ctx, req),
		"consent given under the replaced set must not count toward the new set's threshold")

	f.warpPastWindow()
	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.ErrorIs(t, err, types.ErrRecoveryBelowQuorum,
		"a rotation must withdraw the consent of the guardians it rotated out")

	doc, found := f.k.GetIdentity(f.ctx, f.did)
	require.True(t, found)
	require.Equal(t, f.oldCtrl, doc.Controller)
}

// The other half: the request is not dead, it simply needs consent from the set that is now in force.
func TestRecovery_FreshApprovalsUnderTheNewGuardianSetDoCount(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)
	f.approveToThreshold(t, id)

	f.rotateGuardians(t, "fresh-guardian", 3)

	for i := 0; i < 3; i++ {
		require.NoError(t, f.approveAsLabelled(t, id, "fresh-guardian", i))
	}
	req, found := f.k.GetRecoveryRequest(f.ctx, id)
	require.True(t, found)
	require.Len(t, f.k.EffectiveApprovals(f.ctx, req), 3, "the new set's consent counts")

	f.warpPastWindow()
	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.NoError(t, err, "consent from the set in force must execute")

	doc, found := f.k.GetIdentity(f.ctx, f.did)
	require.True(t, found)
	require.Equal(t, f.newCtrl, doc.Controller)
}

// The retired tally is cleared, not merely ignored: the first guardian to act after a rotation starts a fresh count rather than topping up a stale one.
func TestRecovery_RotationClearsTheStaleTallyRatherThanToppingItUp(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)

	require.NoError(t, f.approve(t, id, 0))
	require.NoError(t, f.approve(t, id, 1))

	f.rotateGuardians(t, "fresh-guardian", 3)

	require.NoError(t, f.approveAsLabelled(t, id, "fresh-guardian", 0))
	req, found := f.k.GetRecoveryRequest(f.ctx, id)
	require.True(t, found)
	require.Len(t, f.k.EffectiveApprovals(f.ctx, req), 1,
		"a rotation must not leave old approvals to be topped up to the new threshold")

	f.warpPastWindow()
	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.ErrorIs(t, err, types.ErrRecoveryBelowQuorum)
}

// Rejections are tallied the same way, so a rotation cannot be undone by stale rejections either.
func TestRecovery_RejectionsFromASupersededGuardianSetDoNotCount(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)

	require.NoError(t, f.reject(t, id, 0))
	require.NoError(t, f.reject(t, id, 1))

	f.rotateGuardians(t, "fresh-guardian", 3)

	req, found := f.k.GetRecoveryRequest(f.ctx, id)
	require.True(t, found)
	require.Empty(t, f.k.EffectiveRejections(f.ctx, req), "rejections from the replaced set are retired too")

	require.NoError(t, f.rejectAsLabelled(t, id, "fresh-guardian", 0))
	req, found = f.k.GetRecoveryRequest(f.ctx, id)
	require.True(t, found, "one rejection under a 3-of-5 set must not settle the request")
	require.Len(t, f.k.EffectiveRejections(f.ctx, req), 1)
}

func (f *recoveryFixture) rejectAsLabelled(t *testing.T, id []byte, label string, i int) error {
	t.Helper()
	_, err := f.msg.RejectRecovery(f.ctx, &types.MsgRejectRecovery{
		Creator:     guardianCtrlLabelled(label, i),
		RecoveryId:  id,
		GuardianDid: f.guardians[i],
		Salt:        saltFor(fmt.Sprintf("%s-%d", label, i)),
	})
	return err
}

// A guardian who is in BOTH sets must be able to consent again after a rotation: their earlier consent was retired with the old set, so re-approving is not a duplicate.
func TestRecovery_AGuardianInBothSetsMayApproveAgainAfterRotation(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)
	require.NoError(t, f.approve(t, id, 0))

	commitments := make([][]byte, 0, len(f.guardians))
	for i, did := range f.guardians {
		commitments = append(commitments, commitFor(did, fmt.Sprintf("guardian-%d", i)))
	}
	_, err := f.msg.SetGuardians(f.ctx, &types.MsgSetGuardians{
		Controller: f.oldCtrl, Did: f.did, Commitments: commitments, Threshold: 3,
	})
	require.NoError(t, err)

	require.NoError(t, f.approve(t, id, 0), "consent retired by a rotation may be given again")
	req, found := f.k.GetRecoveryRequest(f.ctx, id)
	require.True(t, found)
	require.Len(t, f.k.EffectiveApprovals(f.ctx, req), 1)
}

// A DID whose guardian set is never replaced must behave exactly as before — the epoch machinery is inert unless a rotation actually happens.
func TestRecovery_UnrotatedGuardianSetIsUnaffected(t *testing.T) {
	f := setupRecovery(t)
	epochBefore := f.k.GuardianEpoch(f.ctx, f.did)

	id := f.initiate(t, recoveryNonce)
	f.approveToThreshold(t, id)

	req, found := f.k.GetRecoveryRequest(f.ctx, id)
	require.True(t, found)
	require.Len(t, f.k.EffectiveApprovals(f.ctx, req), 3)
	require.Equal(t, epochBefore, f.k.GuardianEpoch(f.ctx, f.did),
		"opening and approving a recovery must not move the guardian-set epoch")

	f.warpPastWindow()
	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.NoError(t, err)
}

// An export and import must not resurrect a retired tally: if the epochs were rebuilt from zero, the stale approvals and the set that superseded them would agree again and the rotation would be undone.
func TestRecovery_RetiredTallyStaysRetiredAcrossGenesis(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)
	f.approveToThreshold(t, id)
	f.rotateGuardians(t, "fresh-guardian", 3)

	gs := f.k.ExportGenesis(f.ctx)
	require.NoError(t, gs.Validate(), "the epoch markers must pass genesis validation")

	var sawGuardianEpoch, sawTallyEpoch bool
	for _, e := range gs.StoreEntries {
		switch {
		case len(e.Key) > 0 && e.Key[0] == types.GuardianEpochPrefix[0]:
			sawGuardianEpoch = true
		case len(e.Key) > 0 && e.Key[0] == types.RecoveryTallyEpochPrefix[0]:
			sawTallyEpoch = true
		}
	}
	require.True(t, sawGuardianEpoch, "the guardian-set epoch must be exported")
	require.True(t, sawTallyEpoch, "the recovery tally epoch must be exported")

	ctx2, k2, _, _ := setupIdentityFull(t, phicryptoAcceptAll())
	ctx2 = ctx2.WithBlockTime(f.ctx.BlockTime())
	k2.InitGenesis(ctx2, *gs)

	req, found := k2.GetRecoveryRequest(ctx2, id)
	require.True(t, found, "the pending request survives the restart")
	require.Empty(t, k2.EffectiveApprovals(ctx2, req),
		"a genesis round trip must not revive approvals a rotation retired")
}
