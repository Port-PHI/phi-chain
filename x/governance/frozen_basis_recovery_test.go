// SPDX-License-Identifier: Apache-2.0

package governance

import (
	"context"
	"crypto/elliptic"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/phicrypto"
	identitykeeper "github.com/Port-PHI/phi-chain/x/identity/keeper"
	identitytypes "github.com/Port-PHI/phi-chain/x/identity/types"

	govkeeper "github.com/Port-PHI/phi-chain/x/governance/keeper"
	govtypes "github.com/Port-PHI/phi-chain/x/governance/types"
)

type e2eBank struct{ balances map[string]math.Int }

func newE2EBank() *e2eBank { return &e2eBank{balances: map[string]math.Int{}} }

func (b *e2eBank) get(k string) math.Int {
	if v, ok := b.balances[k]; ok {
		return v
	}
	return math.ZeroInt()
}

func (b *e2eBank) fund(a sdk.AccAddress, amt math.Int) {
	b.balances["acc:"+a.String()] = b.get("acc:" + a.String()).Add(amt)
}

func (b *e2eBank) move(from, to string, amt sdk.Coins) error {
	v := amt.AmountOf("uphi")
	if b.get(from).LT(v) {
		return fmt.Errorf("insufficient funds: %s has %s, needs %s", from, b.get(from), v)
	}
	b.balances[from] = b.get(from).Sub(v)
	b.balances[to] = b.get(to).Add(v)
	return nil
}

func (b *e2eBank) SendCoinsFromAccountToModule(_ context.Context, sender sdk.AccAddress, mod string, amt sdk.Coins) error {
	return b.move("acc:"+sender.String(), "mod:"+mod, amt)
}

func (b *e2eBank) SendCoinsFromModuleToAccount(_ context.Context, mod string, recipient sdk.AccAddress, amt sdk.Coins) error {
	return b.move("mod:"+mod, "acc:"+recipient.String(), amt)
}

func (b *e2eBank) SendCoinsFromModuleToModule(_ context.Context, from, to string, amt sdk.Coins) error {
	return b.move("mod:"+from, "mod:"+to, amt)
}

const e2eIssuerDID = "did:phi:issuer"

func e2ePubFor(label string) []byte {
	scalar := sha256.Sum256([]byte("phi-test-p256-" + label))
	x, y := elliptic.P256().ScalarBaseMult(scalar[:])
	return elliptic.Marshal(elliptic.P256(), x, y)
}

func e2eDIDFor(t *testing.T, label string) string {
	t.Helper()
	did, err := identitytypes.DeriveDIDFromP256(e2ePubFor(label))
	require.NoError(t, err)
	return did
}

func e2eAddr(s string) string { return sdk.AccAddress([]byte(s)).String() }

type e2eRegistry struct {
	ctx  sdk.Context
	k    identitykeeper.Keeper
	msg  identitytypes.MsgServer
	bank *e2eBank
}

func setupFrozenBasisE2E(t *testing.T) (*e2eRegistry, govkeeper.Keeper) {
	t.Helper()

	idKey := storetypes.NewKVStoreKey(identitytypes.StoreKey)
	govKey := storetypes.NewKVStoreKey(govtypes.StoreKey)
	ctx := testutil.DefaultContextWithKeys(
		map[string]*storetypes.KVStoreKey{identitytypes.StoreKey: idKey, govtypes.StoreKey: govKey},
		map[string]*storetypes.TransientStoreKey{}, map[string]*storetypes.MemoryStoreKey{},
	).WithChainID("phi-testnet-1")

	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	authority := sdk.AccAddress([]byte("gov_authority_______")).String()

	bank := newE2EBank()
	idk := identitykeeper.NewKeeper(cdc, idKey, authority, phicrypto.AcceptAll(), bank)
	require.NoError(t, idk.SetParams(ctx, identitytypes.DefaultParams()))
	idk.SetTrustedIssuer(ctx, identitytypes.TrustedIssuer{
		Did: e2eIssuerDID, PubKey: []byte("issuer-pk"), Active: true,
	})

	gk := govkeeper.NewKeeper(cdc, govKey, authority)
	require.NoError(t, gk.SetParams(ctx, govtypes.DefaultParams()))

	return &e2eRegistry{ctx: ctx, k: idk, msg: identitykeeper.NewMsgServerImpl(idk), bank: bank}, gk
}

func (r *e2eRegistry) at(unix int64) { r.ctx = r.ctx.WithBlockTime(time.Unix(unix, 0)) }

func (r *e2eRegistry) register(t *testing.T, controller, label string) string {
	t.Helper()
	_, err := r.msg.RegisterIdentity(r.ctx, &identitytypes.MsgRegisterIdentity{
		Creator: controller, Did: e2eDIDFor(t, label), PubKey: e2ePubFor(label),
		UniquenessHash: []byte("bio-" + label),
		IssuerDid:      e2eIssuerDID, IssuerSig: []byte("isig"),
		Nonce: []byte("nonce-" + label), PopSig: []byte("pop-" + label),
	})
	require.NoError(t, err)
	return e2eDIDFor(t, label)
}

func e2eSalt(label string) []byte {
	s := make([]byte, identitytypes.GuardianSaltLen)
	copy(s, "salt-"+label)
	return s
}

// TestFrozenBasisE2E_RecoveredOlderDIDCannotEnterAFrozenNumerator is the halt, driven end to end.
func TestFrozenBasisE2E_RecoveredOlderDIDCannotEnterAFrozenNumerator(t *testing.T) {
	reg, gk := setupFrozenBasisE2E(t)

	const day = int64(24 * 60 * 60)
	born := int64(1_000_000)

	reg.at(born)
	victim := e2eAddr("victim-controller___")
	victimDID := reg.register(t, victim, "victim")

	setup := born + 20*day
	reg.at(setup)

	guardianDIDs := make([]string, 5)
	commitments := make([][]byte, 5)
	for i := 0; i < 5; i++ {
		label := fmt.Sprintf("guardian-%d", i)
		guardianDIDs[i] = reg.register(t, e2eAddr(fmt.Sprintf("guardian-ctrl-%-6d", i)), label)
		commitments[i] = identitytypes.GuardianCommitment(guardianDIDs[i], e2eSalt(label))
	}
	_, err := reg.msg.SetGuardians(reg.ctx, &identitytypes.MsgSetGuardians{
		Controller: victim, Did: victimDID, Commitments: commitments, Threshold: 3,
	})
	require.NoError(t, err)

	attacker := e2eAddr("attacker-controller_")
	reg.register(t, attacker, "attacker")

	attackerAddr, err := sdk.AccAddressFromBech32(attacker)
	require.NoError(t, err)
	reg.bank.fund(attackerAddr, reg.k.GetParams(reg.ctx).RecoveryDeposit().MulRaw(10))

	res, err := reg.msg.InitiateRecovery(reg.ctx, &identitytypes.MsgInitiateRecovery{
		Creator: attacker, Did: victimDID, ProposedNewPubKey: e2ePubFor("recovered-key"),
		KeyType: identitytypes.KEY_TYPE_SECP256R1, Method: identitytypes.RECOVERY_METHOD_SOCIAL,
		Nonce: []byte("recovery-nonce-e2e"), PopSig: []byte("pop"),
	})
	require.NoError(t, err)
	recoveryID := res.RecoveryId
	for i := 0; i < 3; i++ {
		_, err = reg.msg.ApproveRecovery(reg.ctx, &identitytypes.MsgApproveRecovery{
			Creator:    e2eAddr(fmt.Sprintf("guardian-ctrl-%-6d", i)),
			RecoveryId: recoveryID, GuardianDid: guardianDIDs[i],
			Salt: e2eSalt(fmt.Sprintf("guardian-%d", i)),
		})
		require.NoError(t, err)
	}

	frozenAt := setup + 3600
	reg.at(frozenAt)
	hooks := NewVoteHooks(gk, nil, reg.k, publicRoutes{}, noValidators{})
	inv := TurnoutWithinFrozenBasisInvariant(gk)
	proposal := proposalAt(7, frozenAt)

	basis := hooks.freezeEligibilityOnce(reg.ctx, proposal)
	require.Equal(t, uint64(1), basis.Denominator,
		"only the aged victim is inside the frozen denominator")

	victimAddr, err := sdk.AccAddressFromBech32(victim)
	require.NoError(t, err)
	require.NoError(t, hooks.recordBallot(reg.ctx, proposal, victim, victimAddr.Bytes(), single(v1.OptionYes)))
	require.Equal(t, uint64(1), gk.GetRunningTally(reg.ctx, proposal.Id).Turnout)

	reg.at(setup + 73*3600)
	_, err = reg.msg.ExecuteRecovery(reg.ctx, &identitytypes.MsgExecuteRecovery{
		Creator: attacker, RecoveryId: recoveryID,
	})
	require.NoError(t, err)

	recovered, found := reg.k.GetIdentity(reg.ctx, victimDID)
	require.True(t, found)
	require.Equal(t, attacker, recovered.Controller, "recovery moved the old DID to the attacker")
	require.Equal(t, born, recovered.CreatedAt, "and it kept its original creation time")

	ballotErr := hooks.recordBallot(reg.ctx, proposal, attacker, attackerAddr.Bytes(), single(v1.OptionNo))

	msg, broken := inv(reg.ctx)
	require.False(t, broken, msg)
	require.LessOrEqual(t, gk.GetRunningTally(reg.ctx, proposal.Id).Turnout, basis.Denominator,
		"turnout must never exceed the denominator it is measured against")
	require.Error(t, ballotErr,
		"a controller outside the frozen denominator must not enter the numerator by acquiring an older DID")
}

// TestFrozenBasisE2E_SuspendReinstateKeepsAVoterAndDoesNotHalt is the suspend/reinstate direction, end to end: a voter counted in a frozen denominator, suspended after the freeze and reinstated during the voting period, can STILL cast its ballot — and the turnout invariant never breaks.
func TestFrozenBasisE2E_SuspendReinstateKeepsAVoterAndDoesNotHalt(t *testing.T) {
	reg, gk := setupFrozenBasisE2E(t)

	const day = int64(24 * 60 * 60)
	born := int64(1_000_000)

	reg.at(born)
	voter := e2eAddr("voter-controller____")
	voterDID := reg.register(t, voter, "voter")
	auth := reg.k.GetAuthority()

	frozenAt := born + 20*day
	reg.at(frozenAt)
	hooks := NewVoteHooks(gk, nil, reg.k, publicRoutes{}, noValidators{})
	inv := TurnoutWithinFrozenBasisInvariant(gk)
	proposal := proposalAt(9, frozenAt)

	basis := hooks.freezeEligibilityOnce(reg.ctx, proposal)
	require.Equal(t, uint64(1), basis.Denominator, "the aged voter is inside the frozen denominator")

	reg.at(frozenAt + day)
	_, err := reg.msg.UpdateStatus(reg.ctx, &identitytypes.MsgUpdateStatus{
		Authority: auth, Did: voterDID, NewStatus: identitytypes.DID_STATUS_SUSPENDED,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1), reg.k.CountEligibleControllersAt(reg.ctx, time.Unix(basis.Cutoff, 0), 0),
		"a suspended voter stays counted in the denominator")

	reg.at(frozenAt + 2*day)
	_, err = reg.msg.UpdateStatus(reg.ctx, &identitytypes.MsgUpdateStatus{
		Authority: auth, Did: voterDID, NewStatus: identitytypes.DID_STATUS_ACTIVE,
	})
	require.NoError(t, err)

	voterAddr, err := sdk.AccAddressFromBech32(voter)
	require.NoError(t, err)
	require.NoError(t, hooks.recordBallot(reg.ctx, proposal, voter, voterAddr.Bytes(), single(v1.OptionYes)),
		"a voter counted at the freeze must still vote after a suspend/reinstate round trip")

	msg, broken := inv(reg.ctx)
	require.False(t, broken, msg)
	require.Equal(t, uint64(1), gk.GetRunningTally(reg.ctx, proposal.Id).Turnout)
	require.LessOrEqual(t, gk.GetRunningTally(reg.ctx, proposal.Id).Turnout, basis.Denominator)
}
