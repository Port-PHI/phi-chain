// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/phicrypto"
	credentialstypes "github.com/Port-PHI/phi-chain/x/credentials/types"
	"github.com/Port-PHI/phi-chain/x/voting/keeper"
	"github.com/Port-PHI/phi-chain/x/voting/types"
)

// fakeCredentials is an in-memory x/credentials stub exposing templates.
type fakeCredentials struct {
	templates map[string]credentialstypes.CredentialTemplate
}

func (f *fakeCredentials) GetTemplate(_ sdk.Context, id string) (credentialstypes.CredentialTemplate, bool) {
	t, ok := f.templates[id]
	return t, ok
}

func (f *fakeCredentials) addTemplate(id, owner string, bbsKey []byte) {
	f.templates[id] = credentialstypes.CredentialTemplate{
		Id: id, Version: 1, OwnerDid: owner, IssuerBbsPubkey: bbsKey,
		Status: credentialstypes.TEMPLATE_STATUS_ACTIVE,
	}
}

type fixture struct {
	ctx   sdk.Context
	k     keeper.Keeper
	msg   types.MsgServer
	creds *fakeCredentials
	now   int64
}

func acc(s string) string { return sdk.AccAddress([]byte(s)).String() }

const (
	creatorAddr = "creator_____________"
	voterAddr   = "voter_______________"
	otherAddr   = "other_______________"
	tmplID      = "phi.kyc.v1"
	issuerDID   = "did:phi:issuer"
)

// setup builds a fixture with the voting-soundness gate ENFORCED, so the standard tests exercise the
// post-SNARK behaviour. The dedicated gate test uses setupSound(..., false).
func setup(t *testing.T, verifier phicrypto.Verifier) fixture {
	return setupSound(t, verifier, true)
}

func setupSound(t *testing.T, verifier phicrypto.Verifier, soundnessEnforced bool) fixture {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_vote"))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	creds := &fakeCredentials{templates: map[string]credentialstypes.CredentialTemplate{}}
	creds.addTemplate(tmplID, issuerDID, []byte("issuer-bbs-pubkey"))
	authority := sdk.AccAddress([]byte("gov_authority_______")).String()

	k := keeper.NewKeeper(cdc, key, authority, creds, verifier, soundnessEnforced)
	now := int64(1_000_000)
	ctx := testCtx.Ctx.WithBlockTime(time.Unix(now, 0))
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	return fixture{ctx: ctx, k: k, msg: keeper.NewMsgServerImpl(k), creds: creds, now: now}
}

// createElection opens a standard election running for one day from now.
func (f fixture) createElection(t *testing.T, id string) {
	t.Helper()
	_, err := f.msg.CreateElection(f.ctx, &types.MsgCreateElection{
		Creator: acc(creatorAddr), Id: id, Title: "Q?", Options: []string{"yes", "no"},
		RequiredTemplateId: tmplID, VotingStart: 0, VotingEnd: f.now + 86400,
	})
	require.NoError(t, err)
}

func castMsg(id string, nullifier string, option uint32) *types.MsgCastVote {
	return &types.MsgCastVote{
		Voter: acc(voterAddr), ElectionId: id, Nullifier: []byte(nullifier),
		EligibilityProof: []byte("proof"), OptionIndex: option,
	}
}

// --- create ---

func TestVoting_CreateElection(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.createElection(t, "e1")

	e, ok := f.k.GetElection(f.ctx, "e1")
	require.True(t, ok)
	require.Equal(t, types.ELECTION_STATUS_OPEN, e.Status)
	require.Len(t, e.OptionTallies, 2)
	require.Equal(t, issuerDID, e.RequiredIssuerDid)

	// duplicate id rejected
	_, err := f.msg.CreateElection(f.ctx, &types.MsgCreateElection{
		Creator: acc(creatorAddr), Id: "e1", Title: "Q?", Options: []string{"a", "b"},
		RequiredTemplateId: tmplID, VotingEnd: f.now + 86400,
	})
	require.ErrorIs(t, err, types.ErrElectionExists)
}

func TestVoting_CreateElection_UnknownTemplate(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	_, err := f.msg.CreateElection(f.ctx, &types.MsgCreateElection{
		Creator: acc(creatorAddr), Id: "e1", Title: "Q?", Options: []string{"a", "b"},
		RequiredTemplateId: "ghost", VotingEnd: f.now + 86400,
	})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
}

func TestVoting_CreateElection_TemplateNoKey(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.creds.addTemplate("nokey", issuerDID, nil)
	_, err := f.msg.CreateElection(f.ctx, &types.MsgCreateElection{
		Creator: acc(creatorAddr), Id: "e1", Title: "Q?", Options: []string{"a", "b"},
		RequiredTemplateId: "nokey", VotingEnd: f.now + 86400,
	})
	require.ErrorIs(t, err, types.ErrTemplateMissingKey)
}

// --- cast ---

func TestVoting_CastVote_Valid(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.createElection(t, "e1")

	_, err := f.msg.CastVote(f.ctx, castMsg("e1", "null-1", 0))
	require.NoError(t, err)

	e, _ := f.k.GetElection(f.ctx, "e1")
	require.Equal(t, uint64(1), e.TotalVotes)
	require.Equal(t, uint64(1), e.OptionTallies[0])
	require.Equal(t, uint64(0), e.OptionTallies[1])
	require.True(t, f.k.HasBallot(f.ctx, "e1", []byte("null-1")))
}

// TestVoting_CastVote_RejectedWhenSoundnessNotEnforced is the H-2 regression: until the audited
// derivation-proof SNARK (nullifier = H(secret, election) for a signed-claim secret) is integrated
// into phi-crypto, the binding-only proof is not Sybil-resistant, so CastVote must NOT reach a real
// tally. The build-tag soundness gate (false in the default build) rejects the vote outright.
func TestVoting_CastVote_RejectedWhenSoundnessNotEnforced(t *testing.T) {
	f := setupSound(t, phicrypto.AcceptAll(), false)
	f.createElection(t, "e1")

	_, err := f.msg.CastVote(f.ctx, castMsg("e1", "null-1", 0))
	require.ErrorIs(t, err, types.ErrVotingNotSound)
	require.False(t, f.k.HasBallot(f.ctx, "e1", []byte("null-1")), "no ballot recorded when soundness is off")

	e, _ := f.k.GetElection(f.ctx, "e1")
	require.Equal(t, uint64(0), e.TotalVotes, "no tally when soundness is off")
}

func TestVoting_CastVote_BadProofRejected(t *testing.T) {
	f := setup(t, phicrypto.RejectAll())
	f.createElection(t, "e1")

	_, err := f.msg.CastVote(f.ctx, castMsg("e1", "null-1", 0))
	require.ErrorIs(t, err, types.ErrEligibilityFailed)
	require.False(t, f.k.HasBallot(f.ctx, "e1", []byte("null-1")))
}

func TestVoting_DoubleVoteRejected(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.createElection(t, "e1")

	_, err := f.msg.CastVote(f.ctx, castMsg("e1", "null-1", 0))
	require.NoError(t, err)
	// same nullifier again — rejected
	_, err = f.msg.CastVote(f.ctx, castMsg("e1", "null-1", 1))
	require.ErrorIs(t, err, types.ErrNullifierUsed)

	e, _ := f.k.GetElection(f.ctx, "e1")
	require.Equal(t, uint64(1), e.TotalVotes)
}

// TestVoting_ProofBoundToSingleNullifier proves the Semaphore binding: a proof is accepted only for
// the nullifier it was bound to, so one proof cannot be reused under a second nullifier (two
// nullifiers from one proof are rejected). The verifier models a real bound proof — valid only when
// re-presented under the nullifier it carries (phi-crypto binds proof↔nullifier via bind_nonce).
func TestVoting_ProofBoundToSingleNullifier(t *testing.T) {
	verifier := phicrypto.Fake{SemaphoreVoteFn: func(proof, _, _, nullifier, _ []byte) bool {
		return bytes.Equal(proof, nullifier) // the proof is bound to exactly this nullifier
	}}
	f := setup(t, verifier)
	f.createElection(t, "e1")

	// A proof bound to "null-1" is accepted under "null-1".
	m1 := &types.MsgCastVote{Voter: acc(voterAddr), ElectionId: "e1", Nullifier: []byte("null-1"), EligibilityProof: []byte("null-1"), OptionIndex: 0}
	_, err := f.msg.CastVote(f.ctx, m1)
	require.NoError(t, err)

	// The SAME proof presented under a different nullifier is rejected (binding mismatch).
	m2 := &types.MsgCastVote{Voter: acc(voterAddr), ElectionId: "e1", Nullifier: []byte("null-2"), EligibilityProof: []byte("null-1"), OptionIndex: 0}
	_, err = f.msg.CastVote(f.ctx, m2)
	require.ErrorIs(t, err, types.ErrEligibilityFailed)

	e, _ := f.k.GetElection(f.ctx, "e1")
	require.Equal(t, uint64(1), e.TotalVotes)
}

// TestVoting_BallotChoiceIsBound proves the eligibility proof is bound to the chosen option
// (signal), so the same proof + nullifier re-tagged with a different OptionIndex is rejected — a
// relay/aggregator cannot flip a voter's choice. The verifier models a proof whose bytes equal the
// canonical signal (4-byte big-endian option index) of the option the voter committed to.
func TestVoting_BallotChoiceIsBound(t *testing.T) {
	bound := binary.BigEndian.AppendUint32(nil, 1) // the voter committed to option index 1
	verifier := phicrypto.Fake{SemaphoreVoteFn: func(proof, _, _, _, signal []byte) bool {
		return bytes.Equal(proof, signal) // accepted only when the on-chain option matches the bound one
	}}
	f := setup(t, verifier)
	f.createElection(t, "e1") // options ["a","b"]

	// Re-tagged to option 0 → the signal no longer matches the bound proof → rejected.
	bad := &types.MsgCastVote{Voter: acc(voterAddr), ElectionId: "e1", Nullifier: []byte("null-x"), EligibilityProof: bound, OptionIndex: 0}
	_, err := f.msg.CastVote(f.ctx, bad)
	require.ErrorIs(t, err, types.ErrEligibilityFailed)

	// Cast under the bound option 1 → accepted.
	good := &types.MsgCastVote{Voter: acc(voterAddr), ElectionId: "e1", Nullifier: []byte("null-x"), EligibilityProof: bound, OptionIndex: 1}
	_, err = f.msg.CastVote(f.ctx, good)
	require.NoError(t, err)
}

// TestVoting_DistinctFreshProofsStillCount documents the residual one-human-one-vote gap that the
// binding layer does NOT close: a holder able to generate multiple fresh, correctly-bound proofs from
// one credential can still mint distinct nullifiers (modeled by AcceptAll). Closing it needs the
// zero-knowledge derivation proof nullifier = H(secret, election) — a vetted SNARK circuit tracked in
// phi-crypto semaphore.rs. (The binding above prevents reuse of a single proof; this is the deeper gap.)
func TestVoting_DistinctFreshProofsStillCount(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.createElection(t, "e1")

	_, err := f.msg.CastVote(f.ctx, castMsg("e1", "null-1", 0))
	require.NoError(t, err)
	_, err = f.msg.CastVote(f.ctx, castMsg("e1", "null-2", 0))
	require.NoError(t, err)

	e, _ := f.k.GetElection(f.ctx, "e1")
	require.Equal(t, uint64(2), e.TotalVotes)
}

func TestVoting_VoteBeforeStart(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	_, err := f.msg.CreateElection(f.ctx, &types.MsgCreateElection{
		Creator: acc(creatorAddr), Id: "e1", Title: "Q?", Options: []string{"a", "b"},
		RequiredTemplateId: tmplID, VotingStart: f.now + 1000, VotingEnd: f.now + 86400,
	})
	require.NoError(t, err)

	_, err = f.msg.CastVote(f.ctx, castMsg("e1", "null-1", 0))
	require.ErrorIs(t, err, types.ErrVotingNotStarted)
}

func TestVoting_VoteAfterEnd(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.createElection(t, "e1")

	late := f.ctx.WithBlockTime(time.Unix(f.now+90000, 0))
	_, err := f.msg.CastVote(late, castMsg("e1", "null-1", 0))
	require.ErrorIs(t, err, types.ErrVotingEnded)
}

func TestVoting_InvalidOption(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.createElection(t, "e1")

	_, err := f.msg.CastVote(f.ctx, castMsg("e1", "null-1", 5))
	require.ErrorIs(t, err, types.ErrInvalidOption)
}

func TestVoting_ProofTooLarge(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	require.NoError(t, f.k.SetParams(f.ctx, types.Params{MaxOptions: 32, MinVotingPeriodSeconds: 3600, MaxProofSizeBytes: 4}))
	f.createElection(t, "e1")

	m := castMsg("e1", "null-1", 0)
	m.EligibilityProof = []byte("this-is-too-long")
	_, err := f.msg.CastVote(f.ctx, m)
	require.ErrorIs(t, err, types.ErrProofTooLarge)
}

func TestVoting_UnknownElection(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	_, err := f.msg.CastVote(f.ctx, castMsg("ghost", "null-1", 0))
	require.ErrorIs(t, err, types.ErrElectionNotFound)
}

// --- close / cancel ---

func TestVoting_CloseStopsVotes(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.createElection(t, "e1")

	// non-creator cannot close
	_, err := f.msg.CloseElection(f.ctx, &types.MsgCloseElection{Creator: acc(otherAddr), ElectionId: "e1"})
	require.ErrorIs(t, err, types.ErrUnauthorized)

	_, err = f.msg.CloseElection(f.ctx, &types.MsgCloseElection{Creator: acc(creatorAddr), ElectionId: "e1"})
	require.NoError(t, err)

	_, err = f.msg.CastVote(f.ctx, castMsg("e1", "null-1", 0))
	require.ErrorIs(t, err, types.ErrElectionNotOpen)
}

func TestVoting_CancelByCreator(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.createElection(t, "e1")

	_, err := f.msg.CancelElection(f.ctx, &types.MsgCancelElection{Creator: acc(otherAddr), ElectionId: "e1"})
	require.ErrorIs(t, err, types.ErrUnauthorized)

	_, err = f.msg.CancelElection(f.ctx, &types.MsgCancelElection{Creator: acc(creatorAddr), ElectionId: "e1"})
	require.NoError(t, err)
	e, _ := f.k.GetElection(f.ctx, "e1")
	require.Equal(t, types.ELECTION_STATUS_CANCELLED, e.Status)
}

func TestVoting_CancelAfterVotesRejected(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.createElection(t, "e1")
	_, err := f.msg.CastVote(f.ctx, castMsg("e1", "null-1", 0))
	require.NoError(t, err)

	_, err = f.msg.CancelElection(f.ctx, &types.MsgCancelElection{Creator: acc(creatorAddr), ElectionId: "e1"})
	require.ErrorIs(t, err, types.ErrElectionHasVotes)
}

// --- params / genesis ---

func TestUpdateParams_OnlyAuthority(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())

	_, err := f.msg.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority: acc(otherAddr), Params: types.DefaultParams(),
	})
	require.Error(t, err)

	p := types.DefaultParams()
	p.MaxOptions = 8
	_, err = f.msg.UpdateParams(f.ctx, &types.MsgUpdateParams{Authority: f.k.GetAuthority(), Params: p})
	require.NoError(t, err)
	require.Equal(t, uint32(8), f.k.GetParams(f.ctx).MaxOptions)
}

func TestGenesis_RoundTrip(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.createElection(t, "e1")
	_, err := f.msg.CastVote(f.ctx, castMsg("e1", "null-1", 0))
	require.NoError(t, err)
	_, err = f.msg.CastVote(f.ctx, castMsg("e1", "null-2", 1))
	require.NoError(t, err)

	exported := f.k.ExportGenesis(f.ctx)
	require.NoError(t, exported.Validate())

	f2 := setup(t, phicrypto.AcceptAll())
	f2.k.InitGenesis(f2.ctx, *exported)
	require.Equal(t, exported, f2.k.ExportGenesis(f2.ctx))
}
