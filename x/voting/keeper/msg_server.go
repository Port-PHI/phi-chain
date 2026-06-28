// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/Port-PHI/phi-chain/x/voting/types"
)

// voteSignal is the canonical encoding of a ballot's chosen option that the eligibility proof is
// bound to: the 4-byte big-endian option index. It MUST match what the voter's client folds
// into phi-crypto's bind_nonce when producing the proof, so a relay cannot re-tag the choice.
func voteSignal(optionIndex uint32) []byte {
	return binary.BigEndian.AppendUint32(nil, optionIndex)
}

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns a MsgServer implementation.
func NewMsgServerImpl(k Keeper) types.MsgServer { return &msgServer{Keeper: k} }

var _ types.MsgServer = msgServer{}

// CreateElection opens a new poll gated by a credential template.
func (k msgServer) CreateElection(goCtx context.Context, msg *types.MsgCreateElection) (*types.MsgCreateElectionResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if k.HasElection(ctx, msg.Id) {
		return nil, errors.Wrapf(types.ErrElectionExists, "id %s", msg.Id)
	}
	params := k.GetParams(ctx)
	if uint32(len(msg.Options)) > params.MaxOptions {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "too many options (max %d)", params.MaxOptions)
	}

	now := ctx.BlockTime().Unix()
	start := msg.VotingStart
	if start == 0 {
		start = now
	}
	if msg.VotingEnd <= start {
		return nil, errors.Wrap(types.ErrInvalidRequest, "voting_end must be after the effective voting_start")
	}
	if msg.VotingEnd-start < params.MinVotingPeriodSeconds {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "voting window shorter than min_voting_period_seconds (%d)", params.MinVotingPeriodSeconds)
	}

	// The eligibility template must exist and carry an issuer BBS+ key, else no
	// ballot could ever be verified. The template's issuer BBS+ key is immutable
	// (x/credentials: rotation creates a new template id), so the key recovered at
	// vote time stays consistent with this election for its whole lifetime.
	tmpl, found := k.credentialsKeeper.GetTemplate(ctx, msg.RequiredTemplateId)
	if !found {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "required_template_id %s not found", msg.RequiredTemplateId)
	}
	if len(tmpl.IssuerBbsPubkey) == 0 {
		return nil, errors.Wrapf(types.ErrTemplateMissingKey, "template %s", msg.RequiredTemplateId)
	}
	if msg.RequiredIssuerDid != "" && msg.RequiredIssuerDid != tmpl.OwnerDid {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "required_issuer_did does not match template owner %s", tmpl.OwnerDid)
	}

	e := types.Election{
		Id:                 msg.Id,
		Creator:            msg.Creator,
		Title:              msg.Title,
		Options:            msg.Options,
		RequiredTemplateId: msg.RequiredTemplateId,
		RequiredIssuerDid:  tmpl.OwnerDid,
		VotingStart:        start,
		VotingEnd:          msg.VotingEnd,
		Status:             types.ELECTION_STATUS_OPEN,
		OptionTallies:      make([]uint64, len(msg.Options)),
		TotalVotes:         0,
		CreatedAt:          now,
	}
	k.SetElection(ctx, e)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeCreateElection,
		sdk.NewAttribute(types.AttributeKeyElectionID, msg.Id),
		sdk.NewAttribute(types.AttributeKeyCreator, msg.Creator),
	))
	return &types.MsgCreateElectionResponse{}, nil
}

// CastVote casts an anonymous, eligibility-proven, nullifier-deduplicated ballot.
func (k msgServer) CastVote(goCtx context.Context, msg *types.MsgCastVote) (*types.MsgCastVoteResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Soundness gate: the Semaphore layer is binding-only until the audited derivation-
	// proof SNARK is integrated, so it is NOT Sybil-resistant (one credential -> many fresh nullifiers
	// -> many votes). Refuse to record any ballot until that circuit ships — enforced via the keeper's
	// build-tag-derived soundnessEnforced flag, not a comment.
	if !k.soundnessEnforced {
		return nil, errors.Wrap(types.ErrVotingNotSound, "real tally disabled until the derivation-proof SNARK is integrated")
	}

	e, found := k.GetElection(ctx, msg.ElectionId)
	if !found {
		return nil, errors.Wrapf(types.ErrElectionNotFound, "id %s", msg.ElectionId)
	}
	if e.Status != types.ELECTION_STATUS_OPEN {
		return nil, errors.Wrapf(types.ErrElectionNotOpen, "status %s", e.Status)
	}
	now := ctx.BlockTime().Unix()
	if now < e.VotingStart {
		return nil, types.ErrVotingNotStarted
	}
	if now > e.VotingEnd {
		return nil, types.ErrVotingEnded
	}
	if int(msg.OptionIndex) >= len(e.Options) {
		return nil, errors.Wrapf(types.ErrInvalidOption, "index %d of %d", msg.OptionIndex, len(e.Options))
	}
	if uint32(len(msg.EligibilityProof)) > k.GetParams(ctx).MaxProofSizeBytes {
		return nil, types.ErrProofTooLarge
	}

	tmpl, found := k.credentialsKeeper.GetTemplate(ctx, e.RequiredTemplateId)
	if !found || len(tmpl.IssuerBbsPubkey) == 0 {
		return nil, errors.Wrapf(types.ErrTemplateMissingKey, "template %s", e.RequiredTemplateId)
	}

	// Nullifier must be fresh for this election (double-vote prevention).
	if k.HasBallot(ctx, msg.ElectionId, msg.Nullifier) {
		return nil, types.ErrNullifierUsed
	}

	// Verify eligibility: phi-crypto binds the proof to (election, nullifier, signal) via
	// semaphore::bind_nonce, so it is accepted only for the exact nullifier AND the exact chosen option
	// (signal) it was bound to. A single proof therefore yields at most one accepted nullifier per
	// election (no third-party replay, no two-nullifiers-from-one-proof), and the ballot choice is
	// non-malleable: re-tagging msg.OptionIndex changes the signal, so the proof no longer verifies.
	// The binding hash lives entirely in phi-crypto — the chain passes (election_id, nullifier,
	// signal), not a nonce.
	if !k.verifier.VerifySemaphoreVote(msg.EligibilityProof, tmpl.IssuerBbsPubkey, []byte(msg.ElectionId), msg.Nullifier, voteSignal(msg.OptionIndex)) {
		return nil, errors.Wrap(types.ErrEligibilityFailed, "BBS+ eligibility proof")
	}

	// Defensive overflow guard (practically unreachable at ~2^64 votes, but avoids
	// silent uint64 wraparound corrupting a tally).
	const maxUint64 = ^uint64(0)
	if e.TotalVotes == maxUint64 || e.OptionTallies[msg.OptionIndex] == maxUint64 {
		return nil, errors.Wrap(types.ErrInvalidRequest, "vote tally overflow")
	}

	k.SetBallot(ctx, types.Ballot{
		ElectionId:  msg.ElectionId,
		Nullifier:   msg.Nullifier,
		OptionIndex: msg.OptionIndex,
		CastAt:      now,
	})
	e.OptionTallies[msg.OptionIndex]++
	e.TotalVotes++
	k.SetElection(ctx, e)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeCastVote,
		sdk.NewAttribute(types.AttributeKeyElectionID, msg.ElectionId),
		sdk.NewAttribute(types.AttributeKeyOptionIndex, fmt.Sprintf("%d", msg.OptionIndex)),
		sdk.NewAttribute(types.AttributeKeyNullifier, hex.EncodeToString(msg.Nullifier)),
		sdk.NewAttribute(types.AttributeKeyTotalVotes, fmt.Sprintf("%d", e.TotalVotes)),
	))
	return &types.MsgCastVoteResponse{}, nil
}

// CloseElection closes a poll early (creator only).
func (k msgServer) CloseElection(goCtx context.Context, msg *types.MsgCloseElection) (*types.MsgCloseElectionResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	e, found := k.GetElection(ctx, msg.ElectionId)
	if !found {
		return nil, errors.Wrapf(types.ErrElectionNotFound, "id %s", msg.ElectionId)
	}
	if e.Creator != msg.Creator {
		return nil, errors.Wrap(types.ErrUnauthorized, "only the creator may close the election")
	}
	if e.Status != types.ELECTION_STATUS_OPEN {
		return nil, errors.Wrapf(types.ErrElectionNotOpen, "status %s", e.Status)
	}

	e.Status = types.ELECTION_STATUS_CLOSED
	k.SetElection(ctx, e)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeCloseElection,
		sdk.NewAttribute(types.AttributeKeyElectionID, msg.ElectionId),
	))
	return &types.MsgCloseElectionResponse{}, nil
}

// CancelElection cancels a poll before any vote (creator only).
func (k msgServer) CancelElection(goCtx context.Context, msg *types.MsgCancelElection) (*types.MsgCancelElectionResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	e, found := k.GetElection(ctx, msg.ElectionId)
	if !found {
		return nil, errors.Wrapf(types.ErrElectionNotFound, "id %s", msg.ElectionId)
	}
	if e.Creator != msg.Creator {
		return nil, errors.Wrap(types.ErrUnauthorized, "only the creator may cancel the election")
	}
	if e.Status != types.ELECTION_STATUS_OPEN {
		return nil, errors.Wrapf(types.ErrElectionNotOpen, "status %s", e.Status)
	}
	if e.TotalVotes > 0 {
		return nil, types.ErrElectionHasVotes
	}

	e.Status = types.ELECTION_STATUS_CANCELLED
	k.SetElection(ctx, e)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeCancelElection,
		sdk.NewAttribute(types.AttributeKeyElectionID, msg.ElectionId),
	))
	return &types.MsgCancelElectionResponse{}, nil
}

// UpdateParams updates module parameters — governance only.
func (k msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if msg.Authority != k.authority {
		return nil, errors.Wrapf(govtypes.ErrInvalidSigner, "expected %s, got %s", k.authority, msg.Authority)
	}
	if err := k.SetParams(ctx, msg.Params); err != nil {
		return nil, err
	}
	return &types.MsgUpdateParamsResponse{}, nil
}
