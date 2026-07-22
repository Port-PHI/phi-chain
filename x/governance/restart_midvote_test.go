// SPDX-License-Identifier: Apache-2.0

package governance

import (
	"testing"
	"time"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/phicrypto"
	govkeeper "github.com/Port-PHI/phi-chain/x/governance/keeper"
	govtypes "github.com/Port-PHI/phi-chain/x/governance/types"
	identitykeeper "github.com/Port-PHI/phi-chain/x/identity/keeper"
	identitytypes "github.com/Port-PHI/phi-chain/x/identity/types"
)

type midVoteChain struct {
	ctx    sdk.Context
	gov    govkeeper.Keeper
	before identitykeeper.Keeper
	after  identitykeeper.Keeper
}

const restartedIdentityStoreKey = identitytypes.StoreKey + "-restarted"

func newMidVoteChain(t *testing.T) *midVoteChain {
	t.Helper()

	govKey := storetypes.NewKVStoreKey(govtypes.StoreKey)
	idKey := storetypes.NewKVStoreKey(identitytypes.StoreKey)
	idKey2 := storetypes.NewKVStoreKey(restartedIdentityStoreKey)
	ctx := testutil.DefaultContextWithKeys(
		map[string]*storetypes.KVStoreKey{
			govtypes.StoreKey: govKey, identitytypes.StoreKey: idKey, restartedIdentityStoreKey: idKey2,
		},
		map[string]*storetypes.TransientStoreKey{}, map[string]*storetypes.MemoryStoreKey{},
	).WithChainID("phi-testnet-1")

	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	authority := sdk.AccAddress([]byte("gov_authority_______")).String()

	gov := govkeeper.NewKeeper(cdc, govKey, authority)
	require.NoError(t, gov.SetParams(ctx, govtypes.DefaultParams()))

	before := identitykeeper.NewKeeper(cdc, idKey, authority, phicrypto.AcceptAll(), nil)
	after := identitykeeper.NewKeeper(cdc, idKey2, authority, phicrypto.AcceptAll(), nil)

	c := &midVoteChain{ctx: ctx, gov: gov, before: before, after: after}
	c.at(1_000_000)
	require.NoError(t, before.SetParams(c.ctx, identitytypes.DefaultParams()))
	require.NoError(t, after.SetParams(c.ctx, identitytypes.DefaultParams()))
	return c
}

func (c *midVoteChain) at(unix int64) { c.ctx = c.ctx.WithBlockTime(time.Unix(unix, 0)) }

func (c *midVoteChain) restart(t *testing.T) {
	t.Helper()
	exported := c.before.ExportGenesis(c.ctx)
	require.NoError(t, exported.Validate())
	require.NotPanics(t, func() { c.after.InitGenesis(c.ctx, *exported) })
}

func activeDID(did, controller string, createdAt int64) identitytypes.DIDDocument {
	return identitytypes.DIDDocument{
		Did: did, Controller: controller, Status: identitytypes.DID_STATUS_ACTIVE,
		CreatedAt: createdAt, PubKey: []byte("pk-" + did), UniquenessHash: []byte("uniq-" + did),
	}
}

// TestRestartMidVote_AnEligibleControllerCanStillVoteAfterARestart is the property: a controller that could vote on an in-flight proposal before the restart can still vote on it afterwards.
func TestRestartMidVote_AnEligibleControllerCanStillVoteAfterARestart(t *testing.T) {
	const day = int64(24 * 60 * 60)
	born := int64(1_000_000)

	c := newMidVoteChain(t)
	c.at(born)

	alice := sdk.AccAddress([]byte("midvote-alice_______")).String()
	bob := sdk.AccAddress([]byte("midvote-bob_________")).String()
	c.before.SetIdentity(c.ctx, activeDID("did:phi:midvote-alice", alice, born))
	c.before.SetIdentity(c.ctx, activeDID("did:phi:midvote-bob", bob, born))
	c.before.SetIdentityCount(c.ctx, 2)

	frozenAt := born + 30*day
	c.at(frozenAt)

	hooks := NewVoteHooks(c.gov, nil, c.before, publicRoutes{}, noValidators{})
	proposal := proposalAt(3, frozenAt)
	basis := hooks.freezeEligibilityOnce(c.ctx, proposal)
	require.Equal(t, uint64(2), basis.Denominator, "both controllers are in the frozen basis")

	aliceAddr, err := sdk.AccAddressFromBech32(alice)
	require.NoError(t, err)
	require.NoError(t, hooks.recordBallot(c.ctx, proposal, alice, aliceAddr.Bytes(), single(v1.OptionYes)))
	require.Equal(t, uint64(1), c.gov.GetRunningTally(c.ctx, proposal.Id).Turnout)

	c.at(frozenAt + 3600)
	c.restart(t)

	hooksAfter := NewVoteHooks(c.gov, nil, c.after, publicRoutes{}, noValidators{})
	bobAddr, err := sdk.AccAddressFromBech32(bob)
	require.NoError(t, err)
	require.NoError(t, hooksAfter.recordBallot(c.ctx, proposal, bob, bobAddr.Bytes(), single(v1.OptionNo)),
		"a controller eligible before the restart must still be able to vote on an in-flight proposal")

	require.LessOrEqual(t, c.gov.GetRunningTally(c.ctx, proposal.Id).Turnout, basis.Denominator)
	msg, broken := TurnoutWithinFrozenBasisInvariant(c.gov)(c.ctx)
	require.False(t, broken, msg)
}

// The restart must not become a way IN either.
func TestRestartMidVote_ARestartDoesNotAdmitAControllerTheFreezeExcluded(t *testing.T) {
	const day = int64(24 * 60 * 60)
	born := int64(2_000_000)

	c := newMidVoteChain(t)
	c.at(born)

	early := sdk.AccAddress([]byte("midvote-early_______")).String()
	c.before.SetIdentity(c.ctx, activeDID("did:phi:midvote-early", early, born))
	c.before.SetIdentityCount(c.ctx, 1)

	frozenAt := born + 30*day
	c.at(frozenAt)

	hooks := NewVoteHooks(c.gov, nil, c.before, publicRoutes{}, noValidators{})
	proposal := proposalAt(4, frozenAt)
	basis := hooks.freezeEligibilityOnce(c.ctx, proposal)
	require.Equal(t, uint64(1), basis.Denominator)

	lateAt := frozenAt + 600
	c.at(lateAt)
	late := sdk.AccAddress([]byte("midvote-late________")).String()
	c.before.SetIdentity(c.ctx, activeDID("did:phi:midvote-late", late, born))
	c.before.SetIdentityCount(c.ctx, 2)

	lateAddr, err := sdk.AccAddressFromBech32(late)
	require.NoError(t, err)
	require.Error(t, hooks.recordBallot(c.ctx, proposal, late, lateAddr.Bytes(), single(v1.OptionYes)),
		"precondition: the latecomer is outside the frozen basis before the restart")

	c.at(lateAt + 3600)
	c.restart(t)

	hooksAfter := NewVoteHooks(c.gov, nil, c.after, publicRoutes{}, noValidators{})
	require.Error(t, hooksAfter.recordBallot(c.ctx, proposal, late, lateAddr.Bytes(), single(v1.OptionYes)),
		"a restart must not admit a controller the freeze excluded")

	msg, broken := TurnoutWithinFrozenBasisInvariant(c.gov)(c.ctx)
	require.False(t, broken, msg)
}
