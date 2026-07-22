// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns a MsgServer implementation.
func NewMsgServerImpl(k Keeper) types.MsgServer {
	return &msgServer{Keeper: k}
}

var _ types.MsgServer = msgServer{}

// RegisterIdentity registers a phi identity (unique self-certifying DID + single-use marker + issuer attestation + PoP; fail-closed).
func (k msgServer) RegisterIdentity(goCtx context.Context, msg *types.MsgRegisterIdentity) (*types.MsgRegisterIdentityResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if k.HasIdentity(ctx, msg.Did) {
		return nil, errors.Wrapf(types.ErrIdentityExists, "did %s", msg.Did)
	}
	if k.HasUniqueness(ctx, msg.UniquenessHash) {
		return nil, errors.Wrap(types.ErrUniquenessUsed, "this human already holds a phi identity")
	}
	if err := k.verifyRegistration(ctx, msg); err != nil {
		return nil, err
	}

	doc := types.DIDDocument{
		Did:            msg.Did,
		Controller:     msg.Creator,
		PubKey:         msg.PubKey,
		UniquenessHash: msg.UniquenessHash,
		CreatedAt:      ctx.BlockTime().Unix(),
		Status:         types.DID_STATUS_ACTIVE,
		// Curve every later signature check verifies against; zero value (UNSPECIFIED) reads as r1.
		KeyType: msg.KeyType,
	}
	k.SetIdentity(ctx, doc)
	k.setUniqueness(ctx, msg.UniquenessHash, msg.Did)
	// Consume the issuer attestation nonce (anti-replay).
	k.markIssuerNonce(ctx, msg.IssuerDid, msg.Nonce)

	count := k.GetIdentityCount(ctx) + 1
	k.SetIdentityCount(ctx, count)
	params := k.GetParams(ctx)
	if params.BootstrapPhase && count >= params.BootstrapThreshold {
		params.BootstrapPhase = false // irreversible latch
		if err := k.SetParams(ctx, params); err != nil {
			return nil, err
		}
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeBootstrapEnded,
			sdk.NewAttribute(types.AttributeKeyCount, fmt.Sprintf("%d", count)),
		))
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRegisterIdentity,
		sdk.NewAttribute(types.AttributeKeyDID, msg.Did),
		sdk.NewAttribute(types.AttributeKeyController, msg.Creator),
		sdk.NewAttribute(types.AttributeKeyIssuerDID, msg.IssuerDid),
	))
	return &types.MsgRegisterIdentityResponse{}, nil
}

// RevokeIdentity revokes a DID: controller or governance only.
func (k msgServer) RevokeIdentity(goCtx context.Context, msg *types.MsgRevokeIdentity) (*types.MsgRevokeIdentityResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	doc, found := k.GetIdentity(ctx, msg.Did)
	if !found {
		return nil, errors.Wrapf(types.ErrIdentityNotFound, "did %s", msg.Did)
	}
	if msg.Creator != doc.Controller && msg.Creator != k.authority {
		return nil, errors.Wrap(types.ErrUnauthorized, "only controller or governance may revoke")
	}
	if doc.Status == types.DID_STATUS_REVOKED {
		return nil, types.ErrIdentityRevoked
	}

	doc.Status = types.DID_STATUS_REVOKED
	k.SetIdentity(ctx, doc)

	// Revocation is TERMINAL: tear down recovery state, guardians and validator binding so genesis export/import stays consistent (genesis refuses these to reference a revoked DID).
	if err := k.terminateRecoveryRequestsForDID(ctx, msg.Did); err != nil {
		return nil, err
	}
	k.DeleteGuardianSet(ctx, msg.Did)
	if valoper, bound := k.ValidatorForDID(ctx, msg.Did); bound {
		k.UnbindValidator(ctx, valoper)
	}
	// Retire tally collected under the removed set.
	k.bumpGuardianEpoch(ctx, msg.Did)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRevokeIdentity,
		sdk.NewAttribute(types.AttributeKeyDID, msg.Did),
	))
	return &types.MsgRevokeIdentityResponse{}, nil
}

// RotateIdentityKey rotates a DID's key: controller authorizes, new-key PoP proven; only pub_key changes.
func (k msgServer) RotateIdentityKey(goCtx context.Context, msg *types.MsgRotateIdentityKey) (*types.MsgRotateIdentityKeyResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	doc, found := k.GetIdentity(ctx, msg.Did)
	if !found {
		return nil, errors.Wrapf(types.ErrIdentityNotFound, "did %s", msg.Did)
	}
	if msg.Creator != doc.Controller {
		return nil, errors.Wrap(types.ErrUnauthorized, "only the current controller may rotate the key")
	}
	if doc.Status != types.DID_STATUS_ACTIVE {
		return nil, errors.Wrap(types.ErrKeyRotation, "cannot rotate the key of a non-active identity")
	}
	// PoP of the new key on THIS identity's stored curve, never the message (fail-closed).
	curve, err := types.CurveForKeyType(doc.KeyType)
	if err != nil {
		return nil, errors.Wrap(types.ErrKeyRotation, err.Error())
	}
	m := rotationMessage(ctx.ChainID(), msg.Did, msg.NewPubKey, msg.Creator)
	if !k.verifier.VerifySignature(curve, msg.NewPubKey, m, msg.PopSig) {
		return nil, errors.Wrap(types.ErrInvalidPoP, "new-key proof-of-possession did not verify")
	}

	// Key-collision guard: the new key must not self-certify ANOTHER registered identity (own did exempt).
	derived, derr := types.DeriveDIDForKeyType(doc.KeyType, msg.NewPubKey)
	if derr != nil {
		return nil, errors.Wrap(types.ErrInvalidPubKey, "new_pub_key is not a valid curve point")
	}
	if derived != doc.Did && k.HasIdentity(ctx, derived) {
		return nil, errors.Wrapf(types.ErrRecoveryKeyCollision, "new key derives the registered did %s", derived)
	}

	doc.PubKey = msg.NewPubKey
	k.SetIdentity(ctx, doc)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRotateIdentityKey,
		sdk.NewAttribute(types.AttributeKeyDID, msg.Did),
		sdk.NewAttribute(types.AttributeKeyController, doc.Controller),
	))
	return &types.MsgRotateIdentityKeyResponse{}, nil
}

// UpdateStatus suspends/reinstates a DID (ACTIVE↔SUSPENDED): governance only; REVOKED is terminal and out of scope.
func (k msgServer) UpdateStatus(goCtx context.Context, msg *types.MsgUpdateStatus) (*types.MsgUpdateStatusResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if msg.Authority != k.authority {
		return nil, errors.Wrapf(govtypes.ErrInvalidSigner, "expected %s, got %s", k.authority, msg.Authority)
	}
	if msg.NewStatus != types.DID_STATUS_ACTIVE && msg.NewStatus != types.DID_STATUS_SUSPENDED {
		return nil, errors.Wrapf(types.ErrInvalidStatusTransition, "new_status must be ACTIVE or SUSPENDED, got %s", msg.NewStatus)
	}

	doc, found := k.GetIdentity(ctx, msg.Did)
	if !found {
		return nil, errors.Wrapf(types.ErrIdentityNotFound, "did %s", msg.Did)
	}
	if doc.Status == types.DID_STATUS_REVOKED {
		return nil, errors.Wrap(types.ErrIdentityRevoked, "a revoked identity is terminal and cannot change status")
	}
	old := doc.Status
	if old == msg.NewStatus {
		return nil, errors.Wrapf(types.ErrInvalidStatusTransition, "identity %s is already %s", msg.Did, old)
	}

	doc.Status = msg.NewStatus
	k.SetIdentity(ctx, doc)

	// Suspend/reinstate pauses/restores the recovery window of the DID's live requests.
	switch {
	case old == types.DID_STATUS_ACTIVE && msg.NewStatus == types.DID_STATUS_SUSPENDED:
		k.freezeLiveRecoveries(ctx, msg.Did)
	case old == types.DID_STATUS_SUSPENDED && msg.NewStatus == types.DID_STATUS_ACTIVE:
		k.thawRecoveries(ctx, msg.Did)
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeStatusChanged,
		sdk.NewAttribute(types.AttributeKeyDID, msg.Did),
		sdk.NewAttribute(types.AttributeKeyOldStatus, old.String()),
		sdk.NewAttribute(types.AttributeKeyNewStatus, msg.NewStatus.String()),
	))
	return &types.MsgUpdateStatusResponse{}, nil
}

// SetGuardians replaces a DID's social-recovery guardian set (full replace): controller-signed, DID ACTIVE.
func (k msgServer) SetGuardians(goCtx context.Context, msg *types.MsgSetGuardians) (*types.MsgSetGuardiansResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	doc, found := k.GetIdentity(ctx, msg.Did)
	if !found {
		return nil, errors.Wrapf(types.ErrIdentityNotFound, "did %s", msg.Did)
	}
	if msg.Controller != doc.Controller {
		return nil, errors.Wrap(types.ErrUnauthorized, "only the current controller may set guardians")
	}
	// Defence in depth: consensus invariant must not depend on the ante status guard being in the chain.
	if doc.Status != types.DID_STATUS_ACTIVE {
		return nil, errors.Wrap(types.ErrInvalidGuardians, "cannot set guardians on a non-active identity")
	}
	if err := types.ValidateGuardianSetBasic(msg.Did, msg.Commitments, msg.Threshold); err != nil {
		return nil, err
	}
	// Governed cap; guardian eligibility is enforced at approval time (commitments are hiding).
	if err := k.validateGuardianSetCap(ctx, msg.Commitments); err != nil {
		return nil, err
	}

	k.SetGuardianSet(ctx, types.GuardianSet{
		Did:         msg.Did,
		Commitments: msg.Commitments,
		Threshold:   msg.Threshold,
		UpdatedAt:   ctx.BlockTime().Unix(),
	})
	// Replacing the set retires consent collected under the previous one.
	k.bumpGuardianEpoch(ctx, msg.Did)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeGuardiansSet,
		sdk.NewAttribute(types.AttributeKeyDID, msg.Did),
		sdk.NewAttribute(types.AttributeKeyGuardianCount, fmt.Sprintf("%d", len(msg.Commitments))),
		sdk.NewAttribute(types.AttributeKeyThreshold, fmt.Sprintf("%d", msg.Threshold)),
	))
	return &types.MsgSetGuardiansResponse{}, nil
}

// UpdateParams updates the module parameters: governance authority only.
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
