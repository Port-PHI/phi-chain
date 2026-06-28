// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"context"
	"encoding/hex"
	"fmt"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/Port-PHI/phi-chain/x/credentials/types"
)

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns a MsgServer implementation.
func NewMsgServerImpl(k Keeper) types.MsgServer { return &msgServer{Keeper: k} }

var _ types.MsgServer = msgServer{}

// --- credential templates ---

// RegisterCredentialTemplate registers a new template (version 1) owned by owner_did.
func (k msgServer) RegisterCredentialTemplate(goCtx context.Context, msg *types.MsgRegisterCredentialTemplate) (*types.MsgRegisterCredentialTemplateResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if k.HasTemplate(ctx, msg.Id) {
		return nil, errors.Wrapf(types.ErrTemplateExists, "id %s", msg.Id)
	}
	if _, err := k.authDID(ctx, msg.OwnerDid, msg.Creator); err != nil {
		return nil, err
	}
	// Validate the issuer BBS public key length: non-empty and bounded, so a template
	// cannot anchor an empty or unbounded key. phi-crypto validates the cryptographic format at
	// proof-verification time.
	if l := len(msg.IssuerBbsPubkey); l == 0 || l > types.MaxBbsPubkeyLen {
		return nil, errors.Wrapf(types.ErrInvalidRequest, "issuer_bbs_pubkey length %d (must be 1..%d)", l, types.MaxBbsPubkeyLen)
	}

	now := ctx.BlockTime().Unix()
	t := types.CredentialTemplate{
		Id:              msg.Id,
		Version:         1,
		OwnerDid:        msg.OwnerDid,
		SchemaHash:      msg.SchemaHash,
		IssuerBbsPubkey: msg.IssuerBbsPubkey,
		Name:            msg.Name,
		Status:          types.TEMPLATE_STATUS_ACTIVE,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	k.SetTemplate(ctx, t)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRegisterTemplate,
		sdk.NewAttribute(types.AttributeKeyTemplateID, msg.Id),
		sdk.NewAttribute(types.AttributeKeyOwnerDID, msg.OwnerDid),
		sdk.NewAttribute(types.AttributeKeyVersion, "1"),
	))
	return &types.MsgRegisterCredentialTemplateResponse{}, nil
}

// UpdateCredentialTemplate bumps an active template's version, schema hash and name.
func (k msgServer) UpdateCredentialTemplate(goCtx context.Context, msg *types.MsgUpdateCredentialTemplate) (*types.MsgUpdateCredentialTemplateResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	t, found := k.GetTemplate(ctx, msg.Id)
	if !found {
		return nil, errors.Wrapf(types.ErrTemplateNotFound, "id %s", msg.Id)
	}
	if _, err := k.authDID(ctx, t.OwnerDid, msg.Creator); err != nil {
		return nil, err
	}
	if t.Status == types.TEMPLATE_STATUS_DEPRECATED {
		return nil, errors.Wrap(types.ErrTemplateDeprecated, "cannot update a deprecated template")
	}

	// issuer_bbs_pubkey is immutable after registration (rotate via a new
	// template id), so it is deliberately not touched here.
	t.Version++
	t.SchemaHash = msg.SchemaHash
	t.Name = msg.Name
	t.UpdatedAt = ctx.BlockTime().Unix()
	k.SetTemplate(ctx, t)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeUpdateTemplate,
		sdk.NewAttribute(types.AttributeKeyTemplateID, msg.Id),
		sdk.NewAttribute(types.AttributeKeyVersion, fmt.Sprintf("%d", t.Version)),
	))
	return &types.MsgUpdateCredentialTemplateResponse{Version: t.Version}, nil
}

// DeprecateCredentialTemplate marks a template deprecated (no new anchors).
func (k msgServer) DeprecateCredentialTemplate(goCtx context.Context, msg *types.MsgDeprecateCredentialTemplate) (*types.MsgDeprecateCredentialTemplateResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	t, found := k.GetTemplate(ctx, msg.Id)
	if !found {
		return nil, errors.Wrapf(types.ErrTemplateNotFound, "id %s", msg.Id)
	}
	if _, err := k.authDID(ctx, t.OwnerDid, msg.Creator); err != nil {
		return nil, err
	}
	if t.Status == types.TEMPLATE_STATUS_DEPRECATED {
		return nil, errors.Wrap(types.ErrTemplateDeprecated, "already deprecated")
	}

	t.Status = types.TEMPLATE_STATUS_DEPRECATED
	t.UpdatedAt = ctx.BlockTime().Unix()
	k.SetTemplate(ctx, t)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeDeprecateTemplate,
		sdk.NewAttribute(types.AttributeKeyTemplateID, msg.Id),
	))
	return &types.MsgDeprecateCredentialTemplateResponse{}, nil
}

// --- credential anchors ---

// AnchorCredential anchors an off-chain verifiable credential. The issuer must
// control issuer_did; the subject DID must be active; the template must exist,
// be active and match the declared version; and the issuer signature over the
// credential hash must verify via phi-crypto.
//
// Subject consent is intentionally NOT required: this is an issuer-anchors
// model — the issuer attests a claim about the subject and is accountable for it, and the subject can
// decline to present the credential off-chain. The anchor stores only the credential hash (no raw
// data, "verify and forget"), so an unwanted anchor discloses nothing about the subject on-chain.
func (k msgServer) AnchorCredential(goCtx context.Context, msg *types.MsgAnchorCredential) (*types.MsgAnchorCredentialResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if k.HasAnchor(ctx, msg.CredentialHash) {
		return nil, errors.Wrapf(types.ErrCredentialExists, "hash %s", hex.EncodeToString(msg.CredentialHash))
	}
	pubKey, err := k.authDID(ctx, msg.IssuerDid, msg.Issuer)
	if err != nil {
		return nil, err
	}
	if err := k.requireActiveDID(ctx, msg.SubjectDid); err != nil {
		return nil, errors.Wrap(err, "subject_did")
	}

	t, found := k.GetTemplate(ctx, msg.TemplateId)
	if !found {
		return nil, errors.Wrapf(types.ErrTemplateNotFound, "id %s", msg.TemplateId)
	}
	if t.Status != types.TEMPLATE_STATUS_ACTIVE {
		return nil, errors.Wrapf(types.ErrTemplateDeprecated, "template %s", msg.TemplateId)
	}
	if msg.TemplateVersion != t.Version {
		return nil, errors.Wrapf(types.ErrTemplateVersionMismatch, "got %d, template at %d", msg.TemplateVersion, t.Version)
	}

	if !k.verifyP256(pubKey, msg.CredentialHash, msg.IssuerSig) {
		return nil, errors.Wrap(types.ErrInvalidSignature, "issuer signature over credential hash")
	}

	a := types.CredentialAnchor{
		CredentialHash:  msg.CredentialHash,
		TemplateId:      msg.TemplateId,
		TemplateVersion: msg.TemplateVersion,
		IssuerDid:       msg.IssuerDid,
		SubjectDid:      msg.SubjectDid,
		IssuedAt:        ctx.BlockTime().Unix(),
		Status:          types.CREDENTIAL_STATUS_ACTIVE,
	}
	k.SetAnchor(ctx, a)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeAnchorCredential,
		sdk.NewAttribute(types.AttributeKeyCredentialHash, hex.EncodeToString(msg.CredentialHash)),
		sdk.NewAttribute(types.AttributeKeyTemplateID, msg.TemplateId),
		sdk.NewAttribute(types.AttributeKeyIssuerDID, msg.IssuerDid),
		sdk.NewAttribute(types.AttributeKeySubjectDID, msg.SubjectDid),
	))
	return &types.MsgAnchorCredentialResponse{}, nil
}

// RevokeCredential revokes a previously anchored credential (issuer only).
func (k msgServer) RevokeCredential(goCtx context.Context, msg *types.MsgRevokeCredential) (*types.MsgRevokeCredentialResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	a, found := k.GetAnchor(ctx, msg.CredentialHash)
	if !found {
		return nil, errors.Wrapf(types.ErrCredentialNotFound, "hash %s", hex.EncodeToString(msg.CredentialHash))
	}
	if a.Status == types.CREDENTIAL_STATUS_REVOKED {
		return nil, types.ErrCredentialRevoked
	}
	if _, err := k.authDID(ctx, a.IssuerDid, msg.Issuer); err != nil {
		return nil, err
	}

	a.Status = types.CREDENTIAL_STATUS_REVOKED
	k.SetAnchor(ctx, a)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRevokeCredential,
		sdk.NewAttribute(types.AttributeKeyCredentialHash, hex.EncodeToString(msg.CredentialHash)),
		sdk.NewAttribute(types.AttributeKeyIssuerDID, a.IssuerDid),
	))
	return &types.MsgRevokeCredentialResponse{}, nil
}

// --- agreements ---

// CreateAgreement creates a multi-party agreement awaiting signatures.
func (k msgServer) CreateAgreement(goCtx context.Context, msg *types.MsgCreateAgreement) (*types.MsgCreateAgreementResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if k.HasAgreement(ctx, msg.Hash) {
		return nil, errors.Wrapf(types.ErrAgreementExists, "hash %s", hex.EncodeToString(msg.Hash))
	}
	if uint32(len(msg.RequiredSigners)) > k.GetParams(ctx).MaxAgreementSigners {
		return nil, errors.Wrap(types.ErrInvalidRequest, "required_signers exceeds max_agreement_signers")
	}

	ag := types.Agreement{
		Hash:            msg.Hash,
		Creator:         msg.Creator,
		RequiredSigners: msg.RequiredSigners,
		Signatures:      []types.AgreementSignature{},
		Deadline:        msg.Deadline,
		Status:          types.AGREEMENT_STATUS_PENDING,
		CreatedAt:       ctx.BlockTime().Unix(),
	}
	k.SetAgreement(ctx, ag)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeCreateAgreement,
		sdk.NewAttribute(types.AttributeKeyAgreementHash, hex.EncodeToString(msg.Hash)),
	))
	return &types.MsgCreateAgreementResponse{}, nil
}

// SignAgreement records a required signer's signature over the agreement hash.
// Only listed DIDs may sign, never twice, never after the deadline, and never
// once the agreement is no longer pending.
func (k msgServer) SignAgreement(goCtx context.Context, msg *types.MsgSignAgreement) (*types.MsgSignAgreementResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	ag, found := k.GetAgreement(ctx, msg.Hash)
	if !found {
		return nil, errors.Wrapf(types.ErrAgreementNotFound, "hash %s", hex.EncodeToString(msg.Hash))
	}
	if ag.Status != types.AGREEMENT_STATUS_PENDING {
		return nil, errors.Wrapf(types.ErrAgreementClosed, "status %s", ag.Status)
	}
	now := ctx.BlockTime().Unix()
	if ag.Deadline != 0 && now > ag.Deadline {
		return nil, types.ErrAgreementExpired
	}
	if !containsString(ag.RequiredSigners, msg.SignerDid) {
		return nil, errors.Wrapf(types.ErrNotRequiredSigner, "did %s", msg.SignerDid)
	}
	for _, s := range ag.Signatures {
		if s.SignerDid == msg.SignerDid {
			return nil, errors.Wrapf(types.ErrAlreadySigned, "did %s", msg.SignerDid)
		}
	}

	pubKey, err := k.authDID(ctx, msg.SignerDid, msg.Signer)
	if err != nil {
		return nil, err
	}
	if !k.verifyP256(pubKey, msg.Hash, msg.Signature) {
		return nil, errors.Wrap(types.ErrInvalidSignature, "signer signature over agreement hash")
	}

	ag.Signatures = append(ag.Signatures, types.AgreementSignature{SignerDid: msg.SignerDid, SignedAt: now})
	completed := len(ag.Signatures) == len(ag.RequiredSigners)
	if completed {
		ag.Status = types.AGREEMENT_STATUS_COMPLETED
	}
	k.SetAgreement(ctx, ag)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeSignAgreement,
		sdk.NewAttribute(types.AttributeKeyAgreementHash, hex.EncodeToString(msg.Hash)),
		sdk.NewAttribute(types.AttributeKeySignerDID, msg.SignerDid),
		sdk.NewAttribute(types.AttributeKeyCompleted, fmt.Sprintf("%t", completed)),
	))
	if completed {
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeCompleteAgreement,
			sdk.NewAttribute(types.AttributeKeyAgreementHash, hex.EncodeToString(msg.Hash)),
		))
	}
	return &types.MsgSignAgreementResponse{Completed: completed}, nil
}

// CancelAgreement cancels a pending agreement (creator only).
func (k msgServer) CancelAgreement(goCtx context.Context, msg *types.MsgCancelAgreement) (*types.MsgCancelAgreementResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	ag, found := k.GetAgreement(ctx, msg.Hash)
	if !found {
		return nil, errors.Wrapf(types.ErrAgreementNotFound, "hash %s", hex.EncodeToString(msg.Hash))
	}
	if ag.Status != types.AGREEMENT_STATUS_PENDING {
		return nil, errors.Wrapf(types.ErrAgreementClosed, "status %s", ag.Status)
	}
	if ag.Creator != msg.Creator {
		return nil, errors.Wrap(types.ErrUnauthorized, "only the creator may cancel")
	}

	ag.Status = types.AGREEMENT_STATUS_CANCELLED
	k.SetAgreement(ctx, ag)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeCancelAgreement,
		sdk.NewAttribute(types.AttributeKeyAgreementHash, hex.EncodeToString(msg.Hash)),
	))
	return &types.MsgCancelAgreementResponse{}, nil
}

// --- personal anchors ---

// AnchorPersonal anchors a self-signed salted hash for its owner (personal vault).
func (k msgServer) AnchorPersonal(goCtx context.Context, msg *types.MsgAnchorPersonal) (*types.MsgAnchorPersonalResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if k.HasPersonalAnchor(ctx, msg.OwnerDid, msg.AnchorHash) {
		return nil, errors.Wrapf(types.ErrPersonalAnchorExists, "hash %s", hex.EncodeToString(msg.AnchorHash))
	}
	pubKey, err := k.authDID(ctx, msg.OwnerDid, msg.Owner)
	if err != nil {
		return nil, err
	}
	if !k.verifyP256(pubKey, msg.AnchorHash, msg.Signature) {
		return nil, errors.Wrap(types.ErrInvalidSignature, "owner signature over anchor hash")
	}

	p := types.PersonalAnchor{
		OwnerDid:   msg.OwnerDid,
		AnchorHash: msg.AnchorHash,
		AnchoredAt: ctx.BlockTime().Unix(),
	}
	k.SetPersonalAnchor(ctx, p)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeAnchorPersonal,
		sdk.NewAttribute(types.AttributeKeyOwnerDID, msg.OwnerDid),
		sdk.NewAttribute(types.AttributeKeyAnchorHash, hex.EncodeToString(msg.AnchorHash)),
	))
	return &types.MsgAnchorPersonalResponse{}, nil
}

// --- params ---

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

// containsString reports whether s is in list.
func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
