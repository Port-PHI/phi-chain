// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/phicrypto"
	"github.com/Port-PHI/phi-chain/x/identity/keeper"
	"github.com/Port-PHI/phi-chain/x/identity/types"
)

type recoveryFixture struct {
	ctx       sdk.Context
	k         keeper.Keeper
	msg       types.MsgServer
	bank      *fakeBank
	oldCtrl   string // the controller that has lost its key
	did       string
	guardians []string // guardian DIDs (revealed only at approval)
	newCtrl   string   // the account of the NEW key — becomes the controller on execute
	newKey    []byte
	deposit   math.Int
	now       time.Time
}

const recoveryNonce = "recovery-nonce-1"

func setupRecovery(t *testing.T) *recoveryFixture {
	t.Helper()
	ctx, k, msg, bank := setupIdentityFull(t, phicryptoAcceptAll())
	now := time.Unix(1_000_000, 0)
	ctx = ctx.WithBlockTime(now)

	oldCtrl := someAddr("lost-key-owner______")
	did := registerActive(t, ctx, msg, oldCtrl, "owner", []byte("bio-owner"))

	guardians, commitments := guardianPool(t, ctx, msg, 5)
	_, err := msg.SetGuardians(ctx, &types.MsgSetGuardians{
		Controller: oldCtrl, Did: did, Commitments: commitments, Threshold: 3,
	})
	require.NoError(t, err)

	newCtrl := someAddr("new-device-account__")
	deposit := k.GetParams(ctx).RecoveryDeposit()
	addr, err := sdk.AccAddressFromBech32(newCtrl)
	require.NoError(t, err)
	bank.Fund(addr, deposit.MulRaw(10))

	return &recoveryFixture{
		ctx: ctx, k: k, msg: msg, bank: bank,
		oldCtrl: oldCtrl, did: did, guardians: guardians,
		newCtrl: newCtrl, newKey: pubFor("recovered-key"),
		deposit: deposit, now: now,
	}
}

func (f *recoveryFixture) initiate(t *testing.T, nonce string) []byte {
	t.Helper()
	res, err := f.msg.InitiateRecovery(f.ctx, f.initiateMsg(nonce, f.newKey))
	require.NoError(t, err)
	return res.RecoveryId
}

func (f *recoveryFixture) initiateMsg(nonce string, key []byte) *types.MsgInitiateRecovery {
	return &types.MsgInitiateRecovery{
		Creator:           f.newCtrl,
		Did:               f.did,
		ProposedNewPubKey: key,
		KeyType:           types.KEY_TYPE_SECP256R1,
		Method:            types.RECOVERY_METHOD_SOCIAL,
		Nonce:             []byte(nonce),
		PopSig:            []byte("pop"),
	}
}

func (f *recoveryFixture) approve(t *testing.T, id []byte, i int) error {
	t.Helper()
	_, err := f.msg.ApproveRecovery(f.ctx, &types.MsgApproveRecovery{
		Creator:     guardianCtrl(i),
		RecoveryId:  id,
		GuardianDid: f.guardians[i],
		Salt:        saltFor(fmt.Sprintf("guardian-%d", i)),
	})
	return err
}

func (f *recoveryFixture) approveToThreshold(t *testing.T, id []byte) {
	t.Helper()
	for i := 0; i < 3; i++ {
		require.NoError(t, f.approve(t, id, i))
	}
}

func (f *recoveryFixture) warpPastWindow() {
	f.ctx = f.ctx.WithBlockTime(f.now.Add(73 * time.Hour))
}

func (f *recoveryFixture) status(t *testing.T, id []byte) types.RecoveryStatus {
	t.Helper()
	r, found := f.k.GetRecoveryRequest(f.ctx, id)
	require.True(t, found)
	return r.Status
}

func (f *recoveryFixture) requireSettled(t *testing.T, id []byte) {
	t.Helper()
	_, found := f.k.GetRecoveryRequest(f.ctx, id)
	require.False(t, found, "a settled recovery request must be pruned from state")
}

func (f *recoveryFixture) feeCollector() math.Int {
	return f.bank.ModuleBalance(types.FeeCollectorName)
}
func (f *recoveryFixture) escrowed() math.Int { return f.bank.ModuleBalance(types.ModuleName) }
func (f *recoveryFixture) newCtrlBalance(t *testing.T) math.Int {
	t.Helper()
	a, err := sdk.AccAddressFromBech32(f.newCtrl)
	require.NoError(t, err)
	return f.bank.BalanceOf(a)
}

// R5 — the consensus-critical invariant.
func TestRecovery_Execute_InvariantAndOldKeyLosesControl(t *testing.T) {
	f := setupRecovery(t)
	before, found := f.k.GetIdentity(f.ctx, f.did)
	require.True(t, found)
	totalBefore := f.bank.Total()

	id := f.initiate(t, recoveryNonce)
	f.approveToThreshold(t, id)
	f.warpPastWindow()

	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{
		Creator: someAddr("any-permissionless__"), RecoveryId: id, // NOT the owner: execute is a crank
	})
	require.NoError(t, err)

	after, found := f.k.GetIdentity(f.ctx, f.did)
	require.True(t, found)

	require.Equal(t, f.newKey, after.PubKey, "pub_key rotated")
	require.Equal(t, f.newCtrl, after.Controller, "controller rotated")

	require.Equal(t, before.Did, after.Did, "the DID identifier is preserved")
	require.Equal(t, before.UniquenessHash, after.UniquenessHash, "the uniqueness marker is preserved")
	require.Equal(t, before.CreatedAt, after.CreatedAt, "created_at (voting age) is preserved")
	require.Equal(t, before.Status, after.Status)

	require.True(t, f.k.HasUniqueness(f.ctx, before.UniquenessHash))

	require.False(t, f.k.IsEligibleControllerAt(f.ctx, f.oldCtrl, f.ctx.BlockTime(), 0),
		"the old controller no longer controls the DID")
	require.True(t, f.k.IsEligibleControllerAt(f.ctx, f.newCtrl, f.ctx.BlockTime(), 0),
		"the new controller now controls the DID")

	f.requireSettled(t, id)

	require.True(t, f.escrowed().IsZero(), "escrow released")
	require.True(t, f.feeCollector().IsZero(), "a successful recovery forfeits nothing")
	require.Equal(t, totalBefore, f.bank.Total(), "every deposit movement is supply-neutral")
}

// R3 — execute before the opposition window has elapsed is rejected, even at full threshold.
func TestRecovery_ExecuteBeforeDelayRejected(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)
	f.approveToThreshold(t, id)

	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.ErrorIs(t, err, types.ErrRecoveryTooEarly)
	require.Equal(t, types.RECOVERY_STATUS_PENDING, f.status(t, id))
}

// R2 — below threshold, execute is rejected even after the window.
func TestRecovery_BelowThresholdRejected(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)
	require.NoError(t, f.approve(t, id, 0))
	require.NoError(t, f.approve(t, id, 1)) // 2 of 3 required
	f.warpPastWindow()

	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.ErrorIs(t, err, types.ErrRecoveryBelowQuorum)
}

// R1 + R7 — a hijack (attacker initiates, guardians even approve) is cancelled by the TRUE owner inside the window: the key is untouched and the deposit is FORFEITED TO THE FEE COLLECTOR (never burned — uphi is vault-backed, so a burn would break solvency).
func TestRecovery_HijackCancelledInWindow_DepositForfeited(t *testing.T) {
	f := setupRecovery(t)
	before, _ := f.k.GetIdentity(f.ctx, f.did)
	totalBefore := f.bank.Total()

	id := f.initiate(t, recoveryNonce)
	f.approveToThreshold(t, id) // the "guardians" were fooled — only the delay saves the owner
	require.Equal(t, f.deposit, f.escrowed())

	_, err := f.msg.CancelRecovery(f.ctx, &types.MsgCancelRecovery{Creator: guardianCtrl(0), RecoveryId: id})
	require.ErrorIs(t, err, types.ErrUnauthorized)
	_, err = f.msg.CancelRecovery(f.ctx, &types.MsgCancelRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.ErrorIs(t, err, types.ErrUnauthorized, "the initiator cannot cancel their own hijack away")

	_, err = f.msg.CancelRecovery(f.ctx, &types.MsgCancelRecovery{Creator: f.oldCtrl, RecoveryId: id})
	require.NoError(t, err)
	f.requireSettled(t, id)

	after, _ := f.k.GetIdentity(f.ctx, f.did)
	require.Equal(t, before.PubKey, after.PubKey, "the key was NOT rotated")
	require.Equal(t, f.oldCtrl, after.Controller, "the owner still controls the DID")

	require.Equal(t, f.deposit, f.feeCollector(), "forfeited deposit lands in the fee collector")
	require.True(t, f.escrowed().IsZero())
	require.Equal(t, totalBefore, f.bank.Total(),
		"forfeit MOVES coins — burning them would break the vault-backed solvency invariant")

	f.warpPastWindow()
	_, err = f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.ErrorIs(t, err, types.ErrRecoveryNotFound)
}

// R8 + R9 — the opening must be correct, and the signer must actually control the revealed guardian.
func TestRecovery_CommitmentMembership(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)

	require.NoError(t, f.approve(t, id, 0))

	_, err := f.msg.ApproveRecovery(f.ctx, &types.MsgApproveRecovery{
		Creator: guardianCtrl(1), RecoveryId: id, GuardianDid: f.guardians[1], Salt: saltFor("wrong"),
	})
	require.ErrorIs(t, err, types.ErrNotAGuardian, "a wrong salt does not open the commitment")

	outsiderCtrl := someAddr("outsider-ctrl_______")
	outsider := registerActive(t, f.ctx, f.msg, outsiderCtrl, "outsider", []byte("bio-outsider"))
	_, err = f.msg.ApproveRecovery(f.ctx, &types.MsgApproveRecovery{
		Creator: outsiderCtrl, RecoveryId: id, GuardianDid: outsider, Salt: saltFor("outsider"),
	})
	require.ErrorIs(t, err, types.ErrNotAGuardian)

	_, err = f.msg.ApproveRecovery(f.ctx, &types.MsgApproveRecovery{
		Creator: someAddr("replayer____________"), RecoveryId: id,
		GuardianDid: f.guardians[0], Salt: saltFor("guardian-0"),
	})
	require.ErrorIs(t, err, types.ErrUnauthorized, "a revealed opening must not be replayable by anyone else")
}

// R4 — a guardian suspended AFTER being enrolled cannot approve: eligibility is judged at approval.
func TestRecovery_SuspendedGuardianApprovalRejected(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)
	auth := f.k.GetAuthority()

	_, err := f.msg.UpdateStatus(f.ctx, &types.MsgUpdateStatus{
		Authority: auth, Did: f.guardians[0], NewStatus: types.DID_STATUS_SUSPENDED,
	})
	require.NoError(t, err)

	require.ErrorIs(t, f.approve(t, id, 0), types.ErrGuardianNotEligible,
		"a suspended guardian cannot approve, even though they were ACTIVE when enrolled")

	_, err = f.msg.RevokeIdentity(f.ctx, &types.MsgRevokeIdentity{Creator: guardianCtrl(1), Did: f.guardians[1]})
	require.NoError(t, err)
	require.ErrorIs(t, f.approve(t, id, 1), types.ErrGuardianNotEligible)
}

// R6 — one guardian's double approval counts once (dedup is by the REVEALED DID).
func TestRecovery_DoubleApprovalCountsOnce(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)

	require.NoError(t, f.approve(t, id, 0))
	require.ErrorIs(t, f.approve(t, id, 0), types.ErrAlreadyApproved)

	r, _ := f.k.GetRecoveryRequest(f.ctx, id)
	require.Len(t, r.Approvals, 1, "a repeated approval must not inflate the count")

	require.NoError(t, f.approve(t, id, 1))
	f.warpPastWindow()
	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.ErrorIs(t, err, types.ErrRecoveryBelowQuorum)
}

// A guardian suspended AFTER approving must NOT void an already-met threshold.
func TestRecovery_LaterSuspensionDoesNotVoidThreshold(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)
	f.approveToThreshold(t, id)

	_, err := f.msg.UpdateStatus(f.ctx, &types.MsgUpdateStatus{
		Authority: f.k.GetAuthority(), Did: f.guardians[0], NewStatus: types.DID_STATUS_SUSPENDED,
	})
	require.NoError(t, err)

	f.warpPastWindow()
	_, err = f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.NoError(t, err, "a threshold already met is not undone by a later suspension")
}

// R14 + the slot cap — a griefer cannot lock a victim out, and the 6th concurrent request is rejected.
func TestRecovery_SlotCap(t *testing.T) {
	f := setupRecovery(t)
	cap := f.k.GetParams(f.ctx).MaxOpenRecoveryRequests
	require.Equal(t, uint32(5), cap)

	for i := 0; i < int(cap); i++ {
		f.initiate(t, fmt.Sprintf("nonce-%d", i))
	}
	require.Equal(t, f.deposit.MulRaw(int64(cap)), f.escrowed(), "each open request escrows a deposit")

	_, err := f.msg.InitiateRecovery(f.ctx, f.initiateMsg("nonce-overflow", f.newKey))
	require.ErrorIs(t, err, types.ErrRecoverySlotsFull)
}

// R12 + expiry — a stale request lazily EXPIRES on next access, its deposit is forfeited to the fee collector, and its slot is reclaimed.
func TestRecovery_LazyExpiry_ForfeitsAndFreesSlot(t *testing.T) {
	f := setupRecovery(t)
	totalBefore := f.bank.Total()
	id := f.initiate(t, recoveryNonce)
	require.Equal(t, f.deposit, f.escrowed())

	ttl := f.k.GetParams(f.ctx).RecoveryRequestTtlSeconds
	f.ctx = f.ctx.WithBlockTime(f.now.Add(time.Duration(ttl+1) * time.Second))

	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.ErrorIs(t, err, types.ErrRecoveryNotPending)
	f.requireSettled(t, id)

	require.Equal(t, f.deposit, f.feeCollector(), "an expired deposit is forfeited to the fee collector")
	require.True(t, f.escrowed().IsZero())
	require.Equal(t, totalBefore, f.bank.Total(), "expiry is supply-neutral")

	_, err = f.msg.InitiateRecovery(f.ctx, f.initiateMsg("nonce-fresh", f.newKey))
	require.NoError(t, err)
}

// R13 — when one request executes, siblings for the same DID are SUPERSEDED and their deposits forfeited; they cannot then execute.
func TestRecovery_SiblingsSuperseded(t *testing.T) {
	f := setupRecovery(t)
	totalBefore := f.bank.Total()

	winner := f.initiate(t, "nonce-winner")
	loserKey := pubFor("other-recovered-key")
	res, err := f.msg.InitiateRecovery(f.ctx, f.initiateMsg("nonce-loser", loserKey))
	require.NoError(t, err)
	loser := res.RecoveryId

	f.approveToThreshold(t, winner)
	f.warpPastWindow()
	_, err = f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: winner})
	require.NoError(t, err)

	f.requireSettled(t, winner)
	f.requireSettled(t, loser)

	_, err = f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: loser})
	require.ErrorIs(t, err, types.ErrRecoveryNotFound)

	require.Equal(t, f.deposit, f.feeCollector())
	require.True(t, f.escrowed().IsZero())
	require.Equal(t, totalBefore, f.bank.Total())
}

// R15 — the same (did, nonce) cannot be replayed.
func TestRecovery_NonceReplayRejected(t *testing.T) {
	f := setupRecovery(t)
	f.initiate(t, recoveryNonce)

	_, err := f.msg.InitiateRecovery(f.ctx, f.initiateMsg(recoveryNonce, f.newKey))
	require.ErrorIs(t, err, types.ErrRecoveryNonceReused)
}

// R10 — a proposed key that already self-certifies a REGISTERED DID is rejected.
func TestRecovery_KeyCollisionRejected(t *testing.T) {
	f := setupRecovery(t)

	collide := pubFor("guardian-0")
	_, err := f.msg.InitiateRecovery(f.ctx, f.initiateMsg("nonce-collide", collide))
	require.ErrorIs(t, err, types.ErrRecoveryKeyCollision)
}

// R11 — a SUSPENDED or REVOKED DID cannot be recovered (recovery must not be a way out of a legal freeze or a terminal revocation).
func TestRecovery_NonActiveDIDNotRecoverable(t *testing.T) {
	f := setupRecovery(t)
	_, err := f.msg.UpdateStatus(f.ctx, &types.MsgUpdateStatus{
		Authority: f.k.GetAuthority(), Did: f.did, NewStatus: types.DID_STATUS_SUSPENDED,
	})
	require.NoError(t, err)

	_, err = f.msg.InitiateRecovery(f.ctx, f.initiateMsg("nonce-suspended", f.newKey))
	require.ErrorIs(t, err, types.ErrInvalidRecovery)
}

// A DID with no guardian set has no SOCIAL recovery path — opening a request that could never reach a threshold would only escrow a deposit and burn a slot.
func TestRecovery_NoGuardiansRejected(t *testing.T) {
	f := setupRecovery(t)
	ctrl := someAddr("no-guardians-owner__")
	did := registerActive(t, f.ctx, f.msg, ctrl, "lonely", []byte("bio-lonely"))

	_, err := f.msg.InitiateRecovery(f.ctx, &types.MsgInitiateRecovery{
		Creator: f.newCtrl, Did: did, ProposedNewPubKey: f.newKey,
		KeyType: types.KEY_TYPE_SECP256R1, Method: types.RECOVERY_METHOD_SOCIAL,
		Nonce: []byte("nonce-lonely"), PopSig: []byte("pop"),
	})
	require.ErrorIs(t, err, types.ErrInvalidGuardians)
}

// R18 — the proof-of-possession is fail-closed: with a rejecting verifier (the tagless build's posture) no recovery can be initiated.
func TestRecovery_PoPFailClosed(t *testing.T) {
	ctx, k, msg, bank := setupIdentityFull(t, phicryptoRejectAll())
	ctx = ctx.WithBlockTime(time.Unix(1_000_000, 0))
	_ = k
	_ = bank

	_, err := msg.InitiateRecovery(ctx, &types.MsgInitiateRecovery{
		Creator: someAddr("new-device-account__"), Did: didFor("nobody"),
		ProposedNewPubKey: pubFor("x"), KeyType: types.KEY_TYPE_SECP256R1,
		Method: types.RECOVERY_METHOD_SOCIAL, Nonce: []byte("n"), PopSig: []byte("pop"),
	})
	require.Error(t, err, "a rejecting verifier must never admit a recovery")
}

// R16 — a PENDING request round-trips through genesis with its ABSOLUTE execute_after preserved, so an export→import never restarts anyone's opposition window.
func TestRecovery_GenesisRoundTrip(t *testing.T) {
	f := setupRecovery(t)
	f.ctx = f.ctx.WithBlockHeight(1)
	id := f.initiate(t, recoveryNonce)
	require.NoError(t, f.approve(t, id, 0))

	want, found := f.k.GetRecoveryRequest(f.ctx, id)
	require.True(t, found)

	gs := f.k.ExportGenesis(f.ctx)
	require.Len(t, gs.RecoveryRequests, 1)
	require.NoError(t, gs.Validate())

	ctx2, k2, _ := setupIdentity(t)
	k2.InitGenesis(ctx2, *gs)

	got, found := k2.GetRecoveryRequest(ctx2, id)
	require.True(t, found)
	require.Equal(t, want.ExecuteAfter, got.ExecuteAfter, "the opposition window is not restarted")
	require.Equal(t, want.ExpiresAt, got.ExpiresAt)
	require.Equal(t, want.Approvals, got.Approvals)
	require.Equal(t, want.ProposedNewController, got.ProposedNewController)
	require.Equal(t, want.DepositUphi, got.DepositUphi)
	require.Equal(t, types.RECOVERY_STATUS_PENDING, got.Status)
}

// L2 — the single-use nonce of a TERMINAL (cancelled/executed/expired) recovery must survive an export→import.
func TestRecovery_TerminalNonceSurvivesGenesisRestart(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)

	_, err := f.msg.CancelRecovery(f.ctx, &types.MsgCancelRecovery{Creator: f.oldCtrl, RecoveryId: id})
	require.NoError(t, err)
	f.requireSettled(t, id)

	gs := f.k.ExportGenesis(f.ctx)
	require.Empty(t, gs.RecoveryRequests, "a terminal request is not exported")
	require.NoError(t, gs.Validate(), "genesis Validate must accept the 0x1A recovery-nonce entries")

	var sawRecoveryNonce bool
	for _, e := range gs.StoreEntries {
		if bytes.HasPrefix(e.Key, types.RecoveryNoncePrefix) {
			sawRecoveryNonce = true
		}
	}
	require.True(t, sawRecoveryNonce, "the burned recovery nonce must be exported")

	ctx2, k2, msg2, _ := setupIdentityFull(t, phicryptoAcceptAll())
	ctx2 = ctx2.WithBlockTime(f.now)
	k2.InitGenesis(ctx2, *gs)
	_, err = msg2.InitiateRecovery(ctx2, f.initiateMsg(recoveryNonce, f.newKey))
	require.ErrorIs(t, err, types.ErrRecoveryNonceReused,
		"a nonce burned before the restart must stay burned after it")
}

func phicryptoAcceptAll() phicrypto.Verifier { return phicrypto.AcceptAll() }
func phicryptoRejectAll() phicrypto.Verifier { return phicrypto.RejectAll() }
