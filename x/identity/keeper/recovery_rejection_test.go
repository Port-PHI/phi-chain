// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

func (f *recoveryFixture) reject(t *testing.T, id []byte, i int) error {
	t.Helper()
	_, err := f.msg.RejectRecovery(f.ctx, &types.MsgRejectRecovery{
		Creator:     guardianCtrl(i),
		RecoveryId:  id,
		GuardianDid: f.guardians[i],
		Salt:        saltFor(fmt.Sprintf("guardian-%d", i)),
	})
	return err
}

func (f *recoveryFixture) rejectToThreshold(t *testing.T, id []byte) {
	t.Helper()
	for i := 0; i < 3; i++ {
		require.NoError(t, f.reject(t, id, i))
	}
}

func (f *recoveryFixture) saturate(t *testing.T, n int) [][]byte {
	t.Helper()
	ids := make([][]byte, 0, n)
	for i := 0; i < n; i++ {
		attacker := someAddr(fmt.Sprintf("squatter-acct-%-6d", i))
		addr := mustAddr(t, attacker)
		f.bank.Fund(addr, f.deposit.MulRaw(4))
		res, err := f.msg.InitiateRecovery(f.ctx, &types.MsgInitiateRecovery{
			Creator:           attacker,
			Did:               f.did,
			ProposedNewPubKey: pubFor(fmt.Sprintf("squatter-key-%d", i)),
			KeyType:           types.KEY_TYPE_SECP256R1,
			Method:            types.RECOVERY_METHOD_SOCIAL,
			Nonce:             []byte(fmt.Sprintf("squatter-nonce-%d", i)),
			PopSig:            []byte("pop"),
		})
		require.NoError(t, err, "squatter %d must be able to open a request — that is the vulnerability", i)
		ids = append(ids, res.RecoveryId)
	}
	return ids
}

func mustAddr(t *testing.T, bech string) sdk.AccAddress {
	t.Helper()
	a, err := sdk.AccAddressFromBech32(bech)
	require.NoError(t, err)
	return a
}

func (f *recoveryFixture) warpPastTTL() {
	f.ctx = f.ctx.WithBlockTime(f.now.Add(15 * 24 * time.Hour))
}

// THE VULNERABILITY, PINNED.
func TestRecovery_SaturatedSlotsLockOutTheOwner(t *testing.T) {
	f := setupRecovery(t)
	maxOpen := int(f.k.GetParams(f.ctx).MaxOpenRecoveryRequests)

	ids := f.saturate(t, maxOpen)
	require.Len(t, ids, maxOpen)

	_, err := f.msg.InitiateRecovery(f.ctx, f.initiateMsg("owners-own-nonce", f.newKey))
	require.ErrorIs(t, err, types.ErrRecoverySlotsFull,
		"a saturated DID refuses its own owner — the lockout this mechanism repairs")

	_, err = f.msg.CancelRecovery(f.ctx, &types.MsgCancelRecovery{Creator: f.newCtrl, RecoveryId: ids[0]})
	require.ErrorIs(t, err, types.ErrUnauthorized,
		"the lost-key owner cannot cancel a squatter — cancel is the CURRENT controller's veto")
}

// THE FIX, END TO END.
func TestRecovery_GuardiansClearSaturationAndOwnerRecovers(t *testing.T) {
	f := setupRecovery(t)
	maxOpen := int(f.k.GetParams(f.ctx).MaxOpenRecoveryRequests)
	ids := f.saturate(t, maxOpen)

	_, err := f.msg.InitiateRecovery(f.ctx, f.initiateMsg("owners-own-nonce", f.newKey))
	require.ErrorIs(t, err, types.ErrRecoverySlotsFull)

	for _, id := range ids {
		f.rejectToThreshold(t, id)
		f.requireSettled(t, id)
	}

	ownID := f.initiate(t, "owners-own-nonce")
	f.approveToThreshold(t, ownID)
	f.warpPastWindow()
	_, err = f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: ownID})
	require.NoError(t, err)

	doc, found := f.k.GetIdentity(f.ctx, f.did)
	require.True(t, found)
	require.Equal(t, f.newKey, doc.PubKey, "the owner's key is installed")
	require.Equal(t, f.newCtrl, doc.Controller)
}

// Clearing ONE slot is enough: the owner needs a single free slot, not an empty DID.
func TestRecovery_ClearingOneSlotAdmitsTheOwner(t *testing.T) {
	f := setupRecovery(t)
	ids := f.saturate(t, int(f.k.GetParams(f.ctx).MaxOpenRecoveryRequests))

	f.rejectToThreshold(t, ids[0])

	_, err := f.msg.InitiateRecovery(f.ctx, f.initiateMsg("owners-own-nonce", f.newKey))
	require.NoError(t, err, "one freed slot is all the owner needs")
}

// Below threshold a rejection decides nothing: the request stays PENDING, its slot stays taken, and no coin moves.
func TestRecovery_RejectionsBelowThresholdDoNothing(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)
	escrowBefore, feesBefore := f.escrowed(), f.feeCollector()

	require.NoError(t, f.reject(t, id, 0))
	require.NoError(t, f.reject(t, id, 1)) // 2 of 3 — one short

	req, found := f.k.GetRecoveryRequest(f.ctx, id)
	require.True(t, found)
	require.Equal(t, types.RECOVERY_STATUS_PENDING, req.Status, "below threshold decides nothing")
	require.Equal(t, []string{f.guardians[0], f.guardians[1]}, req.Rejections, "but the tally is recorded")
	require.True(t, escrowBefore.Equal(f.escrowed()), "no coin moves below threshold")
	require.True(t, feesBefore.Equal(f.feeCollector()))

	require.Len(t, f.k.RecoveryRequestsForDID(f.ctx, f.did), 1)

	require.NoError(t, f.reject(t, id, 2))
	f.requireSettled(t, id)
}

// A non-guardian cannot reject, and neither can a guardian who reveals the wrong salt: rejecting requires exactly the commitment opening that approving requires, no weaker.
func TestRecovery_NonGuardianCannotReject(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)

	_, err := f.msg.RejectRecovery(f.ctx, &types.MsgRejectRecovery{
		Creator: someAddr("outsider____________"), RecoveryId: id,
		GuardianDid: didFor("outsider"), Salt: saltFor("outsider"),
	})
	require.ErrorIs(t, err, types.ErrNotAGuardian)

	_, err = f.msg.RejectRecovery(f.ctx, &types.MsgRejectRecovery{
		Creator: guardianCtrl(0), RecoveryId: id,
		GuardianDid: f.guardians[0], Salt: saltFor("not-my-salt"),
	})
	require.ErrorIs(t, err, types.ErrNotAGuardian)
}

// A publicly revealed opening must not be replayable by a bystander: the signer must control the revealed guardian DID.
func TestRecovery_RejectionRequiresControlOfTheGuardian(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)

	_, err := f.msg.RejectRecovery(f.ctx, &types.MsgRejectRecovery{
		Creator: someAddr("bystander___________"), RecoveryId: id,
		GuardianDid: f.guardians[0], Salt: saltFor("guardian-0"),
	})
	require.ErrorIs(t, err, types.ErrUnauthorized)
}

// One guardian counts once.
func TestRecovery_GuardianCannotRejectTwice(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)

	require.NoError(t, f.reject(t, id, 0))
	err := f.reject(t, id, 0)
	require.ErrorIs(t, err, types.ErrAlreadyRejected)

	req, _ := f.k.GetRecoveryRequest(f.ctx, id)
	require.Len(t, req.Rejections, 1, "the duplicate must not be tallied")
	require.Equal(t, types.RECOVERY_STATUS_PENDING, req.Status)
}

// A suspended guardian is not eligible — judged at rejection time, exactly as at approval time.
func TestRecovery_SuspendedGuardianCannotReject(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)

	_, err := f.msg.UpdateStatus(f.ctx, &types.MsgUpdateStatus{
		Authority: f.k.GetAuthority(), Did: f.guardians[0], NewStatus: types.DID_STATUS_SUSPENDED,
	})
	require.NoError(t, err)

	err = f.reject(t, id, 0)
	require.ErrorIs(t, err, types.ErrGuardianNotEligible)
}

// A guardian of one DID has no authority over another DID's requests.
func TestRecovery_GuardianCannotRejectAnotherDIDsRequest(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)

	_, err := f.msg.RejectRecovery(f.ctx, &types.MsgRejectRecovery{
		Creator: guardianCtrl(0), RecoveryId: id,
		GuardianDid: f.guardians[0], Salt: saltFor("guardian-1"), // guardian 0 with guardian 1's salt
	})
	require.ErrorIs(t, err, types.ErrNotAGuardian)
}

// A terminal request takes no further rejections.
func TestRecovery_CannotRejectATerminalRequest(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)
	f.rejectToThreshold(t, id)
	f.requireSettled(t, id)

	err := f.reject(t, id, 3)
	require.ErrorIs(t, err, types.ErrRecoveryNotFound)
}

// An unknown request id is refused rather than silently ignored.
func TestRecovery_RejectUnknownRequest(t *testing.T) {
	f := setupRecovery(t)
	_, err := f.msg.RejectRecovery(f.ctx, &types.MsgRejectRecovery{
		Creator: guardianCtrl(0), RecoveryId: make([]byte, types.RecoveryIDLen),
		GuardianDid: f.guardians[0], Salt: saltFor("guardian-0"),
	})
	require.ErrorIs(t, err, types.ErrRecoveryNotFound)
}

// THE AUTHORITY-NEUTRALITY CLAIM, ASSERTED.
func TestRecovery_ThresholdRejectionGrantsNoPowerWithholdingLacks(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)

	require.NoError(t, f.approve(t, id, 3))
	require.NoError(t, f.approve(t, id, 4))

	f.warpPastWindow()
	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.ErrorIs(t, err, types.ErrRecoveryBelowQuorum,
		"a withholding coalition of the threshold size already blocks the recovery outright")
}

// A DID nobody is attacking behaves exactly as before: the rejection path is inert unless used.
func TestRecovery_UnattackedOwnerPathUnchanged(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)
	f.approveToThreshold(t, id)
	f.warpPastWindow()
	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.NoError(t, err)

	f.requireSettled(t, id)
	doc, found := f.k.GetIdentity(f.ctx, f.did)
	require.True(t, found)
	require.Equal(t, f.newCtrl, doc.Controller, "the recovery executed normally")
}

// A rejected request burns its own nonce but frees the DID: the owner simply initiates again with a fresh nonce.
func TestRecovery_OwnerReinitiatesAfterRejection(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)
	f.rejectToThreshold(t, id)

	_, err := f.msg.InitiateRecovery(f.ctx, f.initiateMsg(recoveryNonce, f.newKey))
	require.ErrorIs(t, err, types.ErrRecoveryNonceReused)

	id2 := f.initiate(t, "a-fresh-nonce")
	f.approveToThreshold(t, id2)
	f.warpPastWindow()
	_, err = f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id2})
	require.NoError(t, err)
}

// A guardian may reject a request that has already gathered approvals — up until it executes.
func TestRecovery_RejectionBeatsAnUnexecutedApprovalThreshold(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)
	f.approveToThreshold(t, id) // approvals already at threshold

	f.rejectToThreshold(t, id)
	f.requireSettled(t, id)

	f.warpPastWindow()
	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.ErrorIs(t, err, types.ErrRecoveryNotFound, "a rejected request is pruned and can no longer execute")
}

// The deposit is MOVED to the fee collector, never burned and never paid to the guardians — the exact path cancel/expire/supersede already use.
func TestRecovery_RejectionForfeitsDepositToFeeCollector(t *testing.T) {
	f := setupRecovery(t)
	totalBefore := f.bank.Total()
	id := f.initiate(t, recoveryNonce)

	require.True(t, f.escrowed().Equal(f.deposit), "the deposit is escrowed in the module account")
	feesBefore := f.feeCollector()
	guardianBefore := f.bank.BalanceOf(mustAddr(t, guardianCtrl(0)))

	f.rejectToThreshold(t, id)

	require.True(t, f.escrowed().IsZero(), "escrow is emptied")
	require.True(t, f.feeCollector().Equal(feesBefore.Add(f.deposit)),
		"the whole deposit lands in the fee collector")
	require.True(t, f.bank.BalanceOf(mustAddr(t, guardianCtrl(0))).Equal(guardianBefore),
		"guardians are paid nothing for rejecting — they must have no incentive to reject")
	require.True(t, f.bank.Total().Equal(totalBefore),
		"supply is unchanged: the deposit is MOVED, never burned")
}

// Saturation cleared by guardians costs the attacker every deposit, and every uphi of it is accounted for in the fee collector.
func TestRecovery_ClearedSaturationForfeitsEveryDeposit(t *testing.T) {
	f := setupRecovery(t)
	maxOpen := int(f.k.GetParams(f.ctx).MaxOpenRecoveryRequests)

	ids := f.saturate(t, maxOpen)
	totalBefore := f.bank.Total()
	feesBefore := f.feeCollector()

	for _, id := range ids {
		f.rejectToThreshold(t, id)
	}

	expected := f.deposit.Mul(math.NewInt(int64(maxOpen)))
	require.True(t, f.feeCollector().Equal(feesBefore.Add(expected)),
		"every squatter's deposit is forfeited: want +%s, got +%s", expected, f.feeCollector().Sub(feesBefore))
	require.True(t, f.escrowed().IsZero())
	require.True(t, f.bank.Total().Equal(totalBefore), "supply unchanged across the whole clear-out")
}

// A rejected request must not be charged twice: a later touch that would reap or supersede it moves no further coin.
func TestRecovery_RejectedRequestIsNotForfeitedTwice(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)
	f.rejectToThreshold(t, id)

	feesAfterReject := f.feeCollector()

	f.warpPastTTL()
	_, err := f.msg.InitiateRecovery(f.ctx, f.initiateMsg("post-ttl-nonce", pubFor("post-ttl-key")))
	require.NoError(t, err)

	require.True(t, f.feeCollector().Equal(feesAfterReject), "a terminal request is never charged again")
	f.requireSettled(t, id)
}

// A mid-rejection request survives an export→import with its tally intact: a restart must not silently reset a request's rejections and hand a griefer their slot back.
func TestRecovery_GenesisRoundTripsRejections(t *testing.T) {
	f := setupRecovery(t)
	f.ctx = f.ctx.WithBlockHeight(1)
	id := f.initiate(t, recoveryNonce)

	require.NoError(t, f.reject(t, id, 0))
	require.NoError(t, f.reject(t, id, 1)) // below threshold, so it is still PENDING and exported

	want, found := f.k.GetRecoveryRequest(f.ctx, id)
	require.True(t, found)
	require.Len(t, want.Rejections, 2)

	gs := f.k.ExportGenesis(f.ctx)
	require.NoError(t, gs.Validate())

	ctx2, k2, _ := setupIdentity(t)
	k2.InitGenesis(ctx2, *gs)

	got, found := k2.GetRecoveryRequest(ctx2, id)
	require.True(t, found)
	require.Equal(t, want.Rejections, got.Rejections, "the rejection tally survives the restart")
	require.Equal(t, want.Approvals, got.Approvals)
	require.Equal(t, types.RECOVERY_STATUS_PENDING, got.Status)
}

// Genesis refuses a tally no live handler could have produced.
func TestRecovery_GenesisRejectsDuplicateRejections(t *testing.T) {
	f := setupRecovery(t)
	f.ctx = f.ctx.WithBlockHeight(1)
	id := f.initiate(t, recoveryNonce)
	require.NoError(t, f.reject(t, id, 0))

	gs := f.k.ExportGenesis(f.ctx)
	require.NoError(t, gs.Validate())

	gs.RecoveryRequests[0].Rejections = []string{f.guardians[0], f.guardians[0]}
	require.Error(t, gs.Validate())
}
