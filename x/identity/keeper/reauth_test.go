//go:build reauth

// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"bytes"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256r1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/keeper"
	"github.com/Port-PHI/phi-chain/x/identity/types"
)

type reauthFixture struct {
	ctx  sdk.Context
	k    keeper.Keeper
	msg  types.MsgServer
	bank *fakeBank

	attestor    *secp256r1.PrivKey // phi-auth: the registered, ACTIVE TrustedIssuer
	attestorDID string
	ownerPriv   *secp256r1.PrivKey // the key that will be LOST
	ownerCtrl   string
	did         string
	uniq        []byte             // the DID's biometric uniqueness marker — the anti-Sybil anchor
	newPriv     *secp256r1.PrivKey // the key being recovered ONTO
	newPub      []byte
	newCtrl     string
	fee         math.Int
	now         time.Time
}

const (
	reauthNonce      = "reauth-nonce-1"
	testAttestorDID  = "did:phi:phi-auth-attestor"
	reauthIssuerNonc = "reg-nonce-1"
)

func addrOf(p *secp256r1.PrivKey) string { return sdk.AccAddress(p.PubKey().Address()).String() }

func registrationMessage(chainID, did string, pubKey, uniq []byte, creator string, nonce []byte) []byte {
	return types.CanonicalMessage("phi-issuer-attestation-v3",
		[]byte(chainID), []byte(did), pubKey, uniq, []byte(creator), nonce)
}

func setupReauth(t *testing.T) *reauthFixture {
	t.Helper()
	ctx, k, msg, bank := setupIdentityFull(t, reauthVerifier())
	now := time.Unix(1_000_000, 0)
	ctx = ctx.WithBlockTime(now)

	attestor, err := secp256r1.GenPrivKey()
	require.NoError(t, err)
	k.SetTrustedIssuer(ctx, types.TrustedIssuer{
		Did: testAttestorDID, PubKey: attestor.PubKey().Bytes(), Active: true,
	})

	ownerPriv, err := secp256r1.GenPrivKey()
	require.NoError(t, err)
	ownerPub := ownerPriv.PubKey().Bytes()
	ownerCtrl := addrOf(ownerPriv)
	did, err := types.DeriveDIDFromP256(ownerPub)
	require.NoError(t, err)
	uniq := []byte("uniqueness-marker-of-this-human")

	regMsg := registrationMessage(ctx.ChainID(), did, ownerPub, uniq, ownerCtrl, []byte(reauthIssuerNonc))
	issuerSig, err := attestor.Sign(regMsg)
	require.NoError(t, err)
	ownerPoP, err := ownerPriv.Sign(regMsg)
	require.NoError(t, err)
	_, err = msg.RegisterIdentity(ctx, &types.MsgRegisterIdentity{
		Creator: ownerCtrl, Did: did, PubKey: ownerPub, UniquenessHash: uniq,
		IssuerDid: testAttestorDID, IssuerSig: issuerSig,
		Nonce: []byte(reauthIssuerNonc), PopSig: ownerPoP,
	})
	require.NoError(t, err)

	newPriv, err := secp256r1.GenPrivKey()
	require.NoError(t, err)
	newCtrl := addrOf(newPriv)
	fee := k.GetParams(ctx).ReauthRecoveryFee()
	newAddr, err := sdk.AccAddressFromBech32(newCtrl)
	require.NoError(t, err)
	bank.Fund(newAddr, fee.MulRaw(10))

	return &reauthFixture{
		ctx: ctx, k: k, msg: msg, bank: bank,
		attestor: attestor, attestorDID: testAttestorDID,
		ownerPriv: ownerPriv, ownerCtrl: ownerCtrl, did: did, uniq: uniq,
		newPriv: newPriv, newPub: newPriv.PubKey().Bytes(), newCtrl: newCtrl,
		fee: fee, now: now,
	}
}

func (f *reauthFixture) initiateMsg(t *testing.T, nonce string) *types.MsgInitiateRecovery {
	t.Helper()
	return f.initiateMsgSignedOver(t, nonce, f.newPub, f.newCtrl, f.uniq)
}

func (f *reauthFixture) initiateMsgSignedOver(t *testing.T, nonce string, attKey []byte, attCtrl string, attUniq []byte) *types.MsgInitiateRecovery {
	t.Helper()
	return f.initiateMsgSignedOverChain(t, f.ctx.ChainID(), nonce, attKey, attCtrl, attUniq)
}

func (f *reauthFixture) initiateMsgSignedOverChain(t *testing.T, chainID, nonce string, attKey []byte, attCtrl string, attUniq []byte) *types.MsgInitiateRecovery {
	t.Helper()
	popMsg := types.SocialRecoveryPoPMessage(f.ctx.ChainID(), f.did, f.newPub, f.newCtrl, []byte(nonce))
	popSig, err := f.newPriv.Sign(popMsg)
	require.NoError(t, err)

	attMsg := types.ReauthAttestationMessage(chainID, f.did, attKey, attCtrl, attUniq, []byte(nonce))
	attSig, err := f.attestor.Sign(attMsg)
	require.NoError(t, err)

	return &types.MsgInitiateRecovery{
		Creator:           f.newCtrl,
		Did:               f.did,
		ProposedNewPubKey: f.newPub,
		KeyType:           types.KEY_TYPE_SECP256R1,
		Method:            types.RECOVERY_METHOD_REAUTH,
		Nonce:             []byte(nonce),
		PopSig:            popSig,
		ReauthAttestation: attSig,
		AttestorDid:       f.attestorDID,
	}
}

func (f *reauthFixture) initiate(t *testing.T, nonce string) []byte {
	t.Helper()
	res, err := f.msg.InitiateRecovery(f.ctx, f.initiateMsg(t, nonce))
	require.NoError(t, err)
	return res.RecoveryId
}

func (f *reauthFixture) warpPastWindow() { f.ctx = f.ctx.WithBlockTime(f.now.Add(73 * time.Hour)) }

func (f *reauthFixture) status(t *testing.T, id []byte) types.RecoveryStatus {
	t.Helper()
	r, found := f.k.GetRecoveryRequest(f.ctx, id)
	require.True(t, found)
	return r.Status
}

func (f *reauthFixture) requireSettled(t *testing.T, id []byte) {
	t.Helper()
	_, found := f.k.GetRecoveryRequest(f.ctx, id)
	require.False(t, found, "a settled recovery request must be pruned from state")
}

func (f *reauthFixture) feeCollector() math.Int { return f.bank.ModuleBalance(types.FeeCollectorName) }
func (f *reauthFixture) escrowed() math.Int     { return f.bank.ModuleBalance(types.ModuleName) }
func (f *reauthFixture) newCtrlBalance(t *testing.T) math.Int {
	t.Helper()
	a, err := sdk.AccAddressFromBech32(f.newCtrl)
	require.NoError(t, err)
	return f.bank.BalanceOf(a)
}

// The gate is ON in this build — that is the premise of every test below.
func TestReauth_EnabledInThisBuild(t *testing.T) {
	require.True(t, keeper.ReauthRecoveryEnabled)
}

// A valid attestation opens a PENDING request with NO guardians and NO approvals, waits out the same 72h opposition window, and then rotates the key — preserving the recovery invariant exactly as SOCIAL does: only pub_key and controller change; the DID, its uniqueness marker and created_at survive byte-for-byte; the old key ends up controlling nothing.
func TestReauth_ExecuteAfterWindow_InvariantAndSolvency(t *testing.T) {
	f := setupReauth(t)
	before, found := f.k.GetIdentity(f.ctx, f.did)
	require.True(t, found)
	totalBefore := f.bank.Total()
	balanceBefore := f.newCtrlBalance(t)

	id := f.initiate(t, reauthNonce)
	req, found := f.k.GetRecoveryRequest(f.ctx, id)
	require.True(t, found)
	require.Equal(t, types.RECOVERY_STATUS_PENDING, req.Status)
	require.Equal(t, types.RECOVERY_METHOD_REAUTH, req.Method)
	require.Empty(t, req.Approvals, "REAUTH is authorised by the attestation — there is no approval step")
	require.Equal(t, f.attestorDID, req.AttestorDid)
	require.Equal(t, f.now.Unix()+int64(72*60*60), req.ExecuteAfter, "the same 72h window as SOCIAL")

	require.Equal(t, "4000000", req.FeeUphi)
	require.Equal(t, "0", req.DepositUphi, "REAUTH posts no deposit")
	require.Equal(t, f.fee, f.feeCollector())
	require.True(t, f.escrowed().IsZero())
	require.Equal(t, balanceBefore.Sub(f.fee), f.newCtrlBalance(t))

	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.ErrorIs(t, err, types.ErrRecoveryTooEarly)

	f.warpPastWindow()
	_, err = f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{
		Creator: someAddr("any-permissionless__"), RecoveryId: id, // execute is a permissionless crank
	})
	require.NoError(t, err)

	after, found := f.k.GetIdentity(f.ctx, f.did)
	require.True(t, found)

	require.Equal(t, f.newPub, after.PubKey, "pub_key rotated")
	require.Equal(t, f.newCtrl, after.Controller, "controller rotated")

	require.Equal(t, before.Did, after.Did, "the DID identifier is preserved")
	require.Equal(t, before.UniquenessHash, after.UniquenessHash, "the uniqueness marker is preserved")
	require.Equal(t, before.CreatedAt, after.CreatedAt, "created_at (voting age) is preserved")
	require.Equal(t, before.Status, after.Status)
	require.True(t, f.k.HasUniqueness(f.ctx, before.UniquenessHash),
		"the marker still maps 1:1 — recovery MOVED an identity, it did not mint one")

	require.False(t, f.k.IsEligibleControllerAt(f.ctx, f.ownerCtrl, f.ctx.BlockTime(), 0),
		"the old controller no longer controls the DID")
	require.True(t, f.k.IsEligibleControllerAt(f.ctx, f.newCtrl, f.ctx.BlockTime(), 0),
		"the new controller now controls the DID")

	f.requireSettled(t, id)

	require.Equal(t, f.fee, f.feeCollector(), "the fee is NOT refunded on execute — it is a fee")
	require.True(t, f.escrowed().IsZero())
	require.Equal(t, balanceBefore.Sub(f.fee), f.newCtrlBalance(t))
	require.Equal(t, totalBefore, f.bank.Total(), "every REAUTH coin movement is supply-neutral")
}

// THE ANTI-SYBIL BINDING.
func TestReauth_AttestationOverWrongUniquenessHash_Rejected(t *testing.T) {
	f := setupReauth(t)

	m := f.initiateMsgSignedOver(t, reauthNonce, f.newPub, f.newCtrl, []byte("some OTHER human's marker"))
	_, err := f.msg.InitiateRecovery(f.ctx, m)
	require.ErrorIs(t, err, types.ErrInvalidReauthAttestation,
		"an attestation that does not cover THIS DID's uniqueness marker proves nothing about this human")

	require.Empty(t, f.k.RecoveryRequestsForDID(f.ctx, f.did))
	require.True(t, f.feeCollector().IsZero(), "a rejected request charges nothing")
}

// CROSS-CHAIN REPLAY.
func TestReauth_AttestationChainBound(t *testing.T) {
	f := setupReauth(t)
	foreign := f.initiateMsgSignedOverChain(t, "phi-mainnet-1", reauthNonce, f.newPub, f.newCtrl, f.uniq)
	_, err := f.msg.InitiateRecovery(f.ctx, foreign)
	require.ErrorIs(t, err, types.ErrInvalidReauthAttestation,
		"an attestation valid on another Phi chain must not verify here")
	require.Empty(t, f.k.RecoveryRequestsForDID(f.ctx, f.did))
	require.True(t, f.feeCollector().IsZero(), "a rejected request charges nothing")

	f2 := setupReauth(t)
	local := f2.initiateMsgSignedOverChain(t, f2.ctx.ChainID(), reauthNonce, f2.newPub, f2.newCtrl, f2.uniq)
	res, err := f2.msg.InitiateRecovery(f2.ctx, local)
	require.NoError(t, err)
	require.Equal(t, types.RECOVERY_STATUS_PENDING, f2.status(t, res.RecoveryId))
}

// The attestor must be a registered, ACTIVE TrustedIssuer, judged NOW.
func TestReauth_AttestorMustBeRegisteredAndActive(t *testing.T) {
	f := setupReauth(t)
	m := f.initiateMsg(t, reauthNonce)
	m.AttestorDid = "did:phi:not-an-issuer"
	_, err := f.msg.InitiateRecovery(f.ctx, m)
	require.ErrorIs(t, err, types.ErrIssuerNotTrusted)

	f2 := setupReauth(t)
	_, err = f2.msg.RevokeTrustedIssuer(f2.ctx, &types.MsgRevokeTrustedIssuer{
		Authority: f2.k.GetAuthority(), Did: f2.attestorDID,
	})
	require.NoError(t, err)
	_, err = f2.msg.InitiateRecovery(f2.ctx, f2.initiateMsg(t, reauthNonce))
	require.ErrorIs(t, err, types.ErrIssuerNotTrusted,
		"a revoked attestor's outstanding attestations must die with it")
	require.True(t, f2.feeCollector().IsZero())
}

// The attestation pins the exact rotation.
func TestReauth_TamperedTargetRejected(t *testing.T) {
	f := setupReauth(t)
	otherPriv, err := secp256r1.GenPrivKey()
	require.NoError(t, err)
	otherKey := otherPriv.PubKey().Bytes()
	m := f.initiateMsgSignedOver(t, reauthNonce, otherKey, f.newCtrl, f.uniq)
	_, err = f.msg.InitiateRecovery(f.ctx, m)
	require.ErrorIs(t, err, types.ErrInvalidReauthAttestation, "the installed key must be the attested key")

	f2 := setupReauth(t)
	m2 := f2.initiateMsgSignedOver(t, reauthNonce, f2.newPub, someAddr("attacker-elsewhere__"), f2.uniq)
	_, err = f2.msg.InitiateRecovery(f2.ctx, m2)
	require.ErrorIs(t, err, types.ErrInvalidReauthAttestation,
		"the account that ends up controlling the DID must be the attested one")

	require.Empty(t, f.k.RecoveryRequestsForDID(f.ctx, f.did))
	require.Empty(t, f2.k.RecoveryRequestsForDID(f2.ctx, f2.did))
}

// Proof-of-possession of the NEW key is still required on the REAUTH path.
func TestReauth_PoPStillRequired(t *testing.T) {
	f := setupReauth(t)
	m := f.initiateMsg(t, reauthNonce)
	m.PopSig = []byte("not a signature at all__________________________________________")
	_, err := f.msg.InitiateRecovery(f.ctx, m)
	require.ErrorIs(t, err, types.ErrInvalidPoP)

	f2 := setupReauth(t)
	m2 := f2.initiateMsg(t, reauthNonce)
	impostor, err := secp256r1.GenPrivKey() //nolint:govet // shadow is intended: this is a second fixture
	require.NoError(t, err)
	popMsg := types.SocialRecoveryPoPMessage(f2.ctx.ChainID(), f2.did, f2.newPub, f2.newCtrl, []byte(reauthNonce))
	badPoP, err := impostor.Sign(popMsg)
	require.NoError(t, err)
	m2.PopSig = badPoP
	_, err = f2.msg.InitiateRecovery(f2.ctx, m2)
	require.ErrorIs(t, err, types.ErrInvalidPoP,
		"a valid attestation does not excuse a missing proof that the new key is actually held")

	require.True(t, f.feeCollector().IsZero())
	require.True(t, f2.feeCollector().IsZero())
}

// The nonce is single-use, per DID, in the registry SOCIAL already uses (0x1A).
func TestReauth_NonceReplayRejected(t *testing.T) {
	f := setupReauth(t)
	replay := f.initiateMsg(t, reauthNonce) // capture the exact bytes an attacker would replay
	id := f.initiate(t, reauthNonce)

	_, err := f.msg.InitiateRecovery(f.ctx, replay)
	require.ErrorIs(t, err, types.ErrRecoveryNonceReused, "the same attestation cannot be used twice")

	_, err = f.msg.CancelRecovery(f.ctx, &types.MsgCancelRecovery{Creator: f.ownerCtrl, RecoveryId: id})
	require.NoError(t, err)
	_, err = f.msg.InitiateRecovery(f.ctx, replay)
	require.ErrorIs(t, err, types.ErrRecoveryNonceReused)
}

// The owner's veto works exactly as it does for SOCIAL — and it is the ONLY defence a REAUTH victim has, since no guardian was ever asked.
func TestReauth_OwnerCancelsInWindow_FeeForfeitedSlotFreed(t *testing.T) {
	f := setupReauth(t)
	before, _ := f.k.GetIdentity(f.ctx, f.did)
	totalBefore := f.bank.Total()
	balanceBefore := f.newCtrlBalance(t)

	id := f.initiate(t, reauthNonce)

	_, err := f.msg.CancelRecovery(f.ctx, &types.MsgCancelRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.ErrorIs(t, err, types.ErrUnauthorized, "the initiator cannot cancel their own hijack away")

	_, err = f.msg.CancelRecovery(f.ctx, &types.MsgCancelRecovery{Creator: f.ownerCtrl, RecoveryId: id})
	require.NoError(t, err)
	f.requireSettled(t, id)

	after, _ := f.k.GetIdentity(f.ctx, f.did)
	require.Equal(t, before.PubKey, after.PubKey, "the key was NOT rotated")
	require.Equal(t, f.ownerCtrl, after.Controller, "the owner still controls the DID")

	require.Equal(t, f.fee, f.feeCollector(), "the 4 φ fee is forfeited, not refunded")
	require.Equal(t, balanceBefore.Sub(f.fee), f.newCtrlBalance(t))
	require.True(t, f.escrowed().IsZero())
	require.Equal(t, totalBefore, f.bank.Total(), "supply conserved — solvency intact")

	id2 := f.initiate(t, "reauth-nonce-2")
	require.Equal(t, types.RECOVERY_STATUS_PENDING, f.status(t, id2))

	f.warpPastWindow()
	_, err = f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.ErrorIs(t, err, types.ErrRecoveryNotFound)
}

// The slot cap is shared across methods and enforced against REAUTH too: state is bounded no matter which door a request comes through.
func TestReauth_SlotCapShared(t *testing.T) {
	f := setupReauth(t)
	max := int(f.k.GetParams(f.ctx).MaxOpenRecoveryRequests)
	for i := 0; i < max; i++ {
		f.initiate(t, "reauth-slot-nonce-"+string(rune('a'+i)))
	}
	_, err := f.msg.InitiateRecovery(f.ctx, f.initiateMsg(t, "reauth-slot-nonce-overflow"))
	require.ErrorIs(t, err, types.ErrRecoverySlotsFull)
}

// There is NO approval step on the REAUTH path.
func TestReauth_TakesNoGuardianApprovals(t *testing.T) {
	f := setupReauth(t)
	id := f.initiate(t, reauthNonce)

	_, err := f.msg.ApproveRecovery(f.ctx, &types.MsgApproveRecovery{
		Creator:     someAddr("a-guardian-somewhere"),
		RecoveryId:  id,
		GuardianDid: "did:phi:some-guardian",
		Salt:        saltFor("guardian-0"),
	})
	require.ErrorIs(t, err, types.ErrInvalidRecovery)

	f.warpPastWindow()
	_, err = f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.NoError(t, err, "REAUTH executes on the attestation + the window alone — no quorum")
}

// A PENDING REAUTH request survives an export/import cycle with its authorisation intact — including the attestor it names and the fee it already paid — and genesis validation accepts it.
func TestReauth_GenesisRoundTrip(t *testing.T) {
	f := setupReauth(t)
	id := f.initiate(t, reauthNonce)

	exported := f.k.ExportGenesis(f.ctx)
	require.NoError(t, exported.Validate())

	var got *types.RecoveryRequest
	for i := range exported.RecoveryRequests {
		if bytes.Equal(exported.RecoveryRequests[i].RecoveryId, id) {
			got = &exported.RecoveryRequests[i]
		}
	}
	require.NotNil(t, got, "the REAUTH request was exported")
	require.Equal(t, types.RECOVERY_METHOD_REAUTH, got.Method)
	require.Equal(t, f.attestorDID, got.AttestorDid)
	require.Equal(t, "4000000", got.FeeUphi)
	require.Equal(t, "0", got.DepositUphi)
	require.Empty(t, got.Approvals)

	ctx2, k2, _, _ := setupIdentityFull(t, reauthVerifier())
	k2.InitGenesis(ctx2, *exported)
	back, found := k2.GetRecoveryRequest(ctx2, id)
	require.True(t, found)
	require.Equal(t, *got, back)

	orphan := *exported
	orphan.RecoveryRequests = append([]types.RecoveryRequest{}, exported.RecoveryRequests...)
	for i := range orphan.RecoveryRequests {
		if bytes.Equal(orphan.RecoveryRequests[i].RecoveryId, id) {
			orphan.RecoveryRequests[i].AttestorDid = ""
		}
	}
	require.Error(t, orphan.Validate(), "a REAUTH request with no attestor must not import")
}

// L2 — the REAUTH replay this closes.
func TestReauth_TerminalNonceSurvivesGenesisRestart(t *testing.T) {
	f := setupReauth(t)
	replay := f.initiateMsg(t, reauthNonce) // the exact bytes an attacker would replay after a restart
	id := f.initiate(t, reauthNonce)

	_, err := f.msg.CancelRecovery(f.ctx, &types.MsgCancelRecovery{Creator: f.ownerCtrl, RecoveryId: id})
	require.NoError(t, err)
	f.requireSettled(t, id)

	exported := f.k.ExportGenesis(f.ctx)
	require.NoError(t, exported.Validate())
	for i := range exported.RecoveryRequests {
		require.NotEqual(t, id, exported.RecoveryRequests[i].RecoveryId, "a terminal request is not exported")
	}
	var sawRecoveryNonce bool
	for _, e := range exported.StoreEntries {
		if bytes.HasPrefix(e.Key, types.RecoveryNoncePrefix) {
			sawRecoveryNonce = true
		}
	}
	require.True(t, sawRecoveryNonce, "the burned recovery nonce must be exported (0x1A round-trip)")

	ctx2, k2, msg2, _ := setupIdentityFull(t, reauthVerifier())
	ctx2 = ctx2.WithBlockTime(f.now)
	k2.InitGenesis(ctx2, *exported)
	_, err = msg2.InitiateRecovery(ctx2, replay)
	require.ErrorIs(t, err, types.ErrRecoveryNonceReused,
		"a REAUTH nonce burned before the restart must stay burned after it — no replay")
}

// A REVOKED or SUSPENDED identity is not recoverable by REAUTH either: recovery must not become a way to walk out of a legal freeze or a terminal revocation.
func TestReauth_NonActiveDIDNotRecoverable(t *testing.T) {
	f := setupReauth(t)
	_, err := f.msg.UpdateStatus(f.ctx, &types.MsgUpdateStatus{
		Authority: f.k.GetAuthority(), Did: f.did, NewStatus: types.DID_STATUS_SUSPENDED,
	})
	require.NoError(t, err)

	_, err = f.msg.InitiateRecovery(f.ctx, f.initiateMsg(t, reauthNonce))
	require.ErrorIs(t, err, types.ErrInvalidRecovery)
	require.True(t, f.feeCollector().IsZero())
}
