// SPDX-License-Identifier: Apache-2.0

//go:build phicrypto_cgo && voting_snark

package keeper_test

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
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

type snarkKAT struct {
	ChainID            string `json:"chain_id"`
	ElectionID         string `json:"election_id"`
	OptionIndex        uint32 `json:"option_index"`
	IssuerBBSPubkeyB64 string `json:"issuer_bbs_pubkey_b64"`
	NullifierB64       string `json:"nullifier_b64"`
	ProofB64           string `json:"proof_b64"`
}

func loadKAT(t *testing.T) (kat snarkKAT, pubkey, nullifier, proof []byte) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "voting_snark_kat.json"))
	require.NoError(t, err, "read KAT fixture")
	require.NoError(t, json.Unmarshal(raw, &kat), "parse KAT fixture")

	pubkey, err = base64.StdEncoding.DecodeString(kat.IssuerBBSPubkeyB64)
	require.NoError(t, err)
	nullifier, err = base64.StdEncoding.DecodeString(kat.NullifierB64)
	require.NoError(t, err)
	proof, err = base64.StdEncoding.DecodeString(kat.ProofB64)
	require.NoError(t, err)
	require.Len(t, nullifier, types.NullifierPointLen, "fixture nullifier must be a 48-byte G1 point")
	return kat, pubkey, nullifier, proof
}

type snarkCreds struct {
	templates map[string]credentialstypes.CredentialTemplate
}

func (c *snarkCreds) GetTemplate(_ sdk.Context, id string) (credentialstypes.CredentialTemplate, bool) {
	t, ok := c.templates[id]
	return t, ok
}

func (c *snarkCreds) add(id, owner string, bbsKey []byte) {
	c.templates[id] = credentialstypes.CredentialTemplate{
		Id: id, Version: 1, OwnerDid: owner, IssuerBbsPubkey: bbsKey,
		Status: credentialstypes.TEMPLATE_STATUS_ACTIVE,
	}
}

type snarkFixture struct {
	ctx sdk.Context
	k   keeper.Keeper
	msg types.MsgServer
	now int64
}

func snarkAcc(s string) string { return sdk.AccAddress([]byte(s)).String() }

const (
	snarkTmplID    = "phi.vote.v1"
	snarkIssuerDID = "did:phi:issuer"
	snarkCreator   = "creator_____________"
	snarkVoter     = "voter_______________"
)

func setupSnark(t *testing.T, issuerPubkey []byte, chainID string) snarkFixture {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_vote_snark"))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	creds := &snarkCreds{templates: map[string]credentialstypes.CredentialTemplate{}}
	creds.add(snarkTmplID, snarkIssuerDID, issuerPubkey)
	authority := sdk.AccAddress([]byte("gov_authority_______")).String()

	require.True(t, keeper.VotingSoundnessEnforced, "voting_snark build must enforce soundness")
	require.True(t, phicrypto.DefaultEnforces(), "phicrypto_cgo build must link the real verifier")
	k := keeper.NewKeeper(cdc, key, authority, creds, phicrypto.Default(), keeper.VotingSoundnessEnforced)

	now := int64(1_000_000)
	ctx := testCtx.Ctx.WithBlockTime(time.Unix(now, 0)).WithChainID(chainID)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))
	return snarkFixture{ctx: ctx, k: k, msg: keeper.NewMsgServerImpl(k), now: now}
}

func (f snarkFixture) createElection(t *testing.T, id string) {
	t.Helper()
	_, err := f.msg.CreateElection(f.ctx, &types.MsgCreateElection{
		Creator: snarkAcc(snarkCreator), Id: id, Title: "Q?",
		Options:            []string{"a", "b", "c"}, // >= 3 so option 1 (bound) and 2 (re-tag) are both valid
		RequiredTemplateId: snarkTmplID, VotingStart: 0, VotingEnd: f.now + 86400,
	})
	require.NoError(t, err)
}

// TestVotingSnark_ValidProofTallies: the KAT proof verifies through the real C-ABI and is tallied.
func TestVotingSnark_ValidProofTallies(t *testing.T) {
	kat, pubkey, nullifier, proof := loadKAT(t)
	f := setupSnark(t, pubkey, kat.ChainID)
	f.createElection(t, kat.ElectionID)

	_, err := f.msg.CastVote(f.ctx, &types.MsgCastVote{
		Voter: snarkAcc(snarkVoter), ElectionId: kat.ElectionID,
		Nullifier: nullifier, EligibilityProof: proof, OptionIndex: kat.OptionIndex,
	})
	require.NoError(t, err, "a valid derivation proof must be accepted")

	e, ok := f.k.GetElection(f.ctx, kat.ElectionID)
	require.True(t, ok)
	require.Equal(t, uint64(1), e.TotalVotes)
	require.Equal(t, uint64(1), e.OptionTallies[kat.OptionIndex])
	require.True(t, f.k.HasBallot(f.ctx, kat.ElectionID, nullifier))
}

// TestVotingSnark_SameNullifierRejected is the Sybil-resistance assertion: a second cast carrying the SAME nullifier N — exactly what a repeated proof from one credential deterministically yields, since N = Hₑ^{m₀} — is rejected as a double vote, so one credential tallies at most once per election.
func TestVotingSnark_SameNullifierRejected(t *testing.T) {
	kat, pubkey, nullifier, proof := loadKAT(t)
	f := setupSnark(t, pubkey, kat.ChainID)
	f.createElection(t, kat.ElectionID)

	_, err := f.msg.CastVote(f.ctx, &types.MsgCastVote{
		Voter: snarkAcc(snarkVoter), ElectionId: kat.ElectionID,
		Nullifier: nullifier, EligibilityProof: proof, OptionIndex: kat.OptionIndex,
	})
	require.NoError(t, err)

	_, err = f.msg.CastVote(f.ctx, &types.MsgCastVote{
		Voter: snarkAcc(snarkVoter), ElectionId: kat.ElectionID,
		Nullifier: nullifier, EligibilityProof: proof, OptionIndex: kat.OptionIndex,
	})
	require.ErrorIs(t, err, types.ErrNullifierUsed, "same nullifier N must collapse to one vote (Sybil-resistance)")

	e, _ := f.k.GetElection(f.ctx, kat.ElectionID)
	require.Equal(t, uint64(1), e.TotalVotes, "the second cast must not tally")
}

// TestVotingSnark_WrongOptionRejected: re-tagging the ballot to a different option changes the signal, which the proof was not bound to, so verification fails.
func TestVotingSnark_WrongOptionRejected(t *testing.T) {
	kat, pubkey, nullifier, proof := loadKAT(t)
	f := setupSnark(t, pubkey, kat.ChainID)
	f.createElection(t, kat.ElectionID)

	_, err := f.msg.CastVote(f.ctx, &types.MsgCastVote{
		Voter: snarkAcc(snarkVoter), ElectionId: kat.ElectionID,
		Nullifier: nullifier, EligibilityProof: proof, OptionIndex: kat.OptionIndex + 1, // bound to OptionIndex
	})
	require.ErrorIs(t, err, types.ErrEligibilityFailed, "a re-tagged option must be rejected")
}

// TestVotingSnark_WrongElectionRejected: the same proof under a different election id fails (Hₑ and the ballot binding are election-specific).
func TestVotingSnark_WrongElectionRejected(t *testing.T) {
	kat, pubkey, nullifier, proof := loadKAT(t)
	f := setupSnark(t, pubkey, kat.ChainID)
	f.createElection(t, "snark-election-OTHER")

	_, err := f.msg.CastVote(f.ctx, &types.MsgCastVote{
		Voter: snarkAcc(snarkVoter), ElectionId: "snark-election-OTHER",
		Nullifier: nullifier, EligibilityProof: proof, OptionIndex: kat.OptionIndex,
	})
	require.ErrorIs(t, err, types.ErrEligibilityFailed, "verifying under a different election must fail")
}

// TestVotingSnark_MalformedNullifierRejected: a wrong-length nullifier is rejected by the on-chain 48-byte shape gate; a 48-byte-but-off-curve/identity nullifier is rejected by the Rust verifier.
func TestVotingSnark_MalformedNullifierRejected(t *testing.T) {
	kat, pubkey, _, proof := loadKAT(t)
	f := setupSnark(t, pubkey, kat.ChainID)
	f.createElection(t, kat.ElectionID)

	_, err := f.msg.CastVote(f.ctx, &types.MsgCastVote{
		Voter: snarkAcc(snarkVoter), ElectionId: kat.ElectionID,
		Nullifier: make([]byte, 32), EligibilityProof: proof, OptionIndex: kat.OptionIndex,
	})
	require.ErrorIs(t, err, types.ErrInvalidNullifier, "a non-48-byte nullifier must be rejected on-chain")

	_, err = f.msg.CastVote(f.ctx, &types.MsgCastVote{
		Voter: snarkAcc(snarkVoter), ElectionId: kat.ElectionID,
		Nullifier: make([]byte, types.NullifierPointLen), EligibilityProof: proof, OptionIndex: kat.OptionIndex,
	})
	require.ErrorIs(t, err, types.ErrEligibilityFailed, "a 48-byte identity/off-curve nullifier must be rejected")
}

// TestVotingSnark_WrongChainRejected: the KAT proof was bound to kat.ChainID; driving CastVote under a DIFFERENT chain-id (everything else — election, nullifier, option, issuer — held equal) must fail through the real C-ABI verifier.
func TestVotingSnark_WrongChainRejected(t *testing.T) {
	kat, pubkey, nullifier, proof := loadKAT(t)

	fok := setupSnark(t, pubkey, kat.ChainID)
	fok.createElection(t, kat.ElectionID)
	_, err := fok.msg.CastVote(fok.ctx, &types.MsgCastVote{
		Voter: snarkAcc(snarkVoter), ElectionId: kat.ElectionID,
		Nullifier: nullifier, EligibilityProof: proof, OptionIndex: kat.OptionIndex,
	})
	require.NoError(t, err, "the proof must tally under the chain-id it was bound to")

	require.NotEqual(t, kat.ChainID, "phi-other-net", "the wrong chain-id must genuinely differ")
	fbad := setupSnark(t, pubkey, "phi-other-net")
	fbad.createElection(t, kat.ElectionID)
	_, err = fbad.msg.CastVote(fbad.ctx, &types.MsgCastVote{
		Voter: snarkAcc(snarkVoter), ElectionId: kat.ElectionID,
		Nullifier: nullifier, EligibilityProof: proof, OptionIndex: kat.OptionIndex,
	})
	require.ErrorIs(t, err, types.ErrEligibilityFailed,
		"a proof bound to one chain-id must not verify under another")
}
