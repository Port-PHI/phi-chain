// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

// This file implements the four sensitive-action messages. All four follow the shared
// "aggregated-approval multisig" pattern: the signer must be an inst_admin; their approval is
// recorded against the content hash; once the threshold is reached the action executes, otherwise
// it stays "pending". The content hash is independent of the signer so distinct approvals over the
// same content accumulate.

// sensitiveResult is the shared result of an attempt at a sensitive action.
type sensitiveResult struct {
	executed  bool
	approvals uint32
	threshold uint32
}

// trySensitive gates the role, records the approval, and reports whether the threshold is reached.
// If execution is ready, the caller must perform the mutation and then clear the approvals.
func (k Keeper) trySensitive(ctx sdk.Context, inst types.Institution, signerBech string, contentHash []byte) (sensitiveResult, error) {
	if err := k.requireRole(ctx, inst, signerBech, types.INSTITUTION_ROLE_ADMIN); err != nil {
		return sensitiveResult{}, err
	}
	signer, err := sdk.AccAddressFromBech32(signerBech)
	if err != nil {
		return sensitiveResult{}, err
	}
	approvals := k.recordApproval(ctx, inst, contentHash, signer)
	threshold := k.effectiveThreshold(ctx, inst)
	return sensitiveResult{executed: approvals >= threshold, approvals: approvals, threshold: threshold}, nil
}

// emitPending emits the "awaiting more signatures" event.
func emitPending(ctx sdk.Context, instID, action string, r sensitiveResult) {
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeActionPending,
		sdk.NewAttribute(types.AttributeKeyInstitution, instID),
		sdk.NewAttribute(types.AttributeKeyAction, action),
		sdk.NewAttribute(types.AttributeKeyApprovals, fmt.Sprintf("%d", r.approvals)),
		sdk.NewAttribute(types.AttributeKeyThreshold, fmt.Sprintf("%d", r.threshold)),
	))
}

// GrantInstitutionRole grants a sub-institution role (sensitive - multisig).
func (k msgServer) GrantInstitutionRole(goCtx context.Context, msg *types.MsgGrantInstitutionRole) (*types.MsgGrantInstitutionRoleResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	inst, found := k.GetInstitution(ctx, msg.Institution)
	if !found {
		return nil, errors.Wrapf(types.ErrInstitutionNotFound, "id %s", msg.Institution)
	}
	ch := contentHashOf([]byte("grant"), []byte(msg.Institution), []byte(msg.Grantee), roleBytes(msg.Role))
	r, err := k.trySensitive(ctx, inst, msg.Signer, ch)
	if err != nil {
		return nil, err
	}
	resp := &types.MsgGrantInstitutionRoleResponse{Executed: r.executed, Approvals: r.approvals, Threshold: r.threshold}
	if !r.executed {
		emitPending(ctx, inst.Id, "grant_role", r)
		return resp, nil
	}

	grantee, err := sdk.AccAddressFromBech32(msg.Grantee)
	if err != nil {
		return nil, err
	}
	k.SetRole(ctx, inst.Id, grantee, msg.Role)
	k.clearApprovals(ctx, inst.Id, ch)
	// Granting ADMIN changes the admin set: invalidate every other pending multisig approval.
	if msg.Role == types.INSTITUTION_ROLE_ADMIN {
		k.bumpAdminEpoch(ctx, inst.Id)
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRoleGranted,
		sdk.NewAttribute(types.AttributeKeyInstitution, inst.Id),
		sdk.NewAttribute(types.AttributeKeyGrantee, msg.Grantee),
		sdk.NewAttribute(types.AttributeKeyRole, msg.Role.String()),
	))
	return resp, nil
}

// RevokeInstitutionRole revokes a sub-institution role (sensitive - multisig).
func (k msgServer) RevokeInstitutionRole(goCtx context.Context, msg *types.MsgRevokeInstitutionRole) (*types.MsgRevokeInstitutionRoleResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	inst, found := k.GetInstitution(ctx, msg.Institution)
	if !found {
		return nil, errors.Wrapf(types.ErrInstitutionNotFound, "id %s", msg.Institution)
	}
	ch := contentHashOf([]byte("revoke"), []byte(msg.Institution), []byte(msg.Grantee))
	r, err := k.trySensitive(ctx, inst, msg.Signer, ch)
	if err != nil {
		return nil, err
	}
	resp := &types.MsgRevokeInstitutionRoleResponse{Executed: r.executed, Approvals: r.approvals, Threshold: r.threshold}
	if !r.executed {
		emitPending(ctx, inst.Id, "revoke_role", r)
		return resp, nil
	}

	grantee, err := sdk.AccAddressFromBech32(msg.Grantee)
	if err != nil {
		return nil, err
	}
	// Revoking an ADMIN shrinks the admin set: invalidate every other pending multisig approval,
	// so sub-threshold approvals captured earlier cannot execute against the now-lower threshold.
	wasAdmin := k.GetRole(ctx, inst.Id, grantee) == types.INSTITUTION_ROLE_ADMIN
	k.DeleteRole(ctx, inst.Id, grantee)
	k.clearApprovals(ctx, inst.Id, ch)
	if wasAdmin {
		k.bumpAdminEpoch(ctx, inst.Id)
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRoleRevoked,
		sdk.NewAttribute(types.AttributeKeyInstitution, inst.Id),
		sdk.NewAttribute(types.AttributeKeyGrantee, msg.Grantee),
	))
	return resp, nil
}

// UpdateInstitutionAppConfig sets the app display config (sensitive).
func (k msgServer) UpdateInstitutionAppConfig(goCtx context.Context, msg *types.MsgUpdateInstitutionAppConfig) (*types.MsgUpdateInstitutionAppConfigResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	inst, found := k.GetInstitution(ctx, msg.Institution)
	if !found {
		return nil, errors.Wrapf(types.ErrInstitutionNotFound, "id %s", msg.Institution)
	}
	ch := contentHashOf([]byte("appconfig"), []byte(msg.Institution), k.cdc.MustMarshal(&msg.Config))
	r, err := k.trySensitive(ctx, inst, msg.Signer, ch)
	if err != nil {
		return nil, err
	}
	resp := &types.MsgUpdateInstitutionAppConfigResponse{Executed: r.executed, Approvals: r.approvals, Threshold: r.threshold}
	if !r.executed {
		emitPending(ctx, inst.Id, "app_config", r)
		return resp, nil
	}

	inst.AppConfig = msg.Config
	k.SetInstitution(ctx, inst)
	k.clearApprovals(ctx, inst.Id, ch)
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeAppConfigSet,
		sdk.NewAttribute(types.AttributeKeyInstitution, inst.Id),
	))
	return resp, nil
}

// UpdateInstitutionParams sets parameters and risk rules (tighten-only - sensitive).
func (k msgServer) UpdateInstitutionParams(goCtx context.Context, msg *types.MsgUpdateInstitutionParams) (*types.MsgUpdateInstitutionParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	inst, found := k.GetInstitution(ctx, msg.Institution)
	if !found {
		return nil, errors.Wrapf(types.ErrInstitutionNotFound, "id %s", msg.Institution)
	}
	// Tighten-only rule: the redeem cap must not fall below the protocol floor (checked before aggregating signatures).
	if err := k.validateParamsTightenOnly(ctx, msg.Params); err != nil {
		return nil, err
	}
	ch := contentHashOf([]byte("params"), []byte(msg.Institution), k.cdc.MustMarshal(&msg.Params))
	r, err := k.trySensitive(ctx, inst, msg.Signer, ch)
	if err != nil {
		return nil, err
	}
	resp := &types.MsgUpdateInstitutionParamsResponse{Executed: r.executed, Approvals: r.approvals, Threshold: r.threshold}
	if !r.executed {
		emitPending(ctx, inst.Id, "params", r)
		return resp, nil
	}

	inst.Params = msg.Params
	k.SetInstitution(ctx, inst)
	k.clearApprovals(ctx, inst.Id, ch)
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeParamsSet,
		sdk.NewAttribute(types.AttributeKeyInstitution, inst.Id),
	))
	return resp, nil
}

// SetInstitutionDepositKey sets the P-256 deposit-signing key (provably-backed mint - sensitive).
func (k msgServer) SetInstitutionDepositKey(goCtx context.Context, msg *types.MsgSetInstitutionDepositKey) (*types.MsgSetInstitutionDepositKeyResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	inst, found := k.GetInstitution(ctx, msg.Institution)
	if !found {
		return nil, errors.Wrapf(types.ErrInstitutionNotFound, "id %s", msg.Institution)
	}
	ch := contentHashOf([]byte("depositkey"), []byte(msg.Institution), msg.DepositPubkey)
	r, err := k.trySensitive(ctx, inst, msg.Signer, ch)
	if err != nil {
		return nil, err
	}
	resp := &types.MsgSetInstitutionDepositKeyResponse{Executed: r.executed, Approvals: r.approvals, Threshold: r.threshold}
	if !r.executed {
		emitPending(ctx, inst.Id, "deposit_key", r)
		return resp, nil
	}

	inst.DepositPubkey = msg.DepositPubkey
	k.SetInstitution(ctx, inst)
	k.clearApprovals(ctx, inst.Id, ch)
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeDepositKeySet,
		sdk.NewAttribute(types.AttributeKeyInstitution, inst.Id),
	))
	return resp, nil
}
