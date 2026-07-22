// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/errors"
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

// fx onboarding: an exchange applies (naming an active financial guarantor), the guarantor approves, then finalize (operator in bootstrap, else a PASSED public x/gov proposal) creates an INSTITUTION_TYPE_FX institution.

// SetFxRequest stores a pending fx onboarding request keyed by fx_id.
func (k Keeper) SetFxRequest(ctx sdk.Context, req types.FxEntryRequest) {
	ctx.KVStore(k.storeKey).Set(types.FxRequestKey(req.FxId), k.cdc.MustMarshal(&req))
}

func (k Keeper) GetFxRequest(ctx sdk.Context, fxID string) (types.FxEntryRequest, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.FxRequestKey(fxID))
	if bz == nil {
		return types.FxEntryRequest{}, false
	}
	var req types.FxEntryRequest
	k.cdc.MustUnmarshal(bz, &req)
	return req, true
}

func (k Keeper) HasFxRequest(ctx sdk.Context, fxID string) bool {
	return ctx.KVStore(k.storeKey).Has(types.FxRequestKey(fxID))
}

func (k Keeper) DeleteFxRequest(ctx sdk.Context, fxID string) {
	ctx.KVStore(k.storeKey).Delete(types.FxRequestKey(fxID))
}

// IterateFxRequests iterates all pending requests; returning true stops.
func (k Keeper) IterateFxRequests(ctx sdk.Context, cb func(types.FxEntryRequest) bool) {
	store := ctx.KVStore(k.storeKey)
	it := storetypes.KVStorePrefixIterator(store, types.FxRequestPrefix)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var req types.FxEntryRequest
		k.cdc.MustUnmarshal(it.Value(), &req)
		if cb(req) {
			break
		}
	}
}

func (k Keeper) isActiveFinancialGuarantor(inst types.Institution) bool {
	isFinancial := inst.InstitutionType == types.INSTITUTION_TYPE_FINANCIAL ||
		inst.InstitutionType == types.INSTITUTION_TYPE_UNSPECIFIED // default financial
	return isFinancial && inst.Status != types.INSTITUTION_STATUS_FROZEN
}

// RequestFxEntry: an exchange applies to join, naming an active financial guarantor.
func (k msgServer) RequestFxEntry(goCtx context.Context, msg *types.MsgRequestFxEntry) (*types.MsgRequestFxEntryResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if msg.FxId == "" {
		return nil, errors.Wrap(types.ErrFxOnboarding, "fx_id must not be empty")
	}
	if len(msg.FxId) > types.MaxInstitutionIDLen {
		return nil, errors.Wrapf(types.ErrIDTooLong, "fx_id length %d > %d", len(msg.FxId), types.MaxInstitutionIDLen)
	}
	if k.HasInstitution(ctx, msg.FxId) {
		return nil, errors.Wrapf(types.ErrInstitutionExists, "id %s already registered", msg.FxId)
	}
	// A mid-removal id is not re-usable until the purge completes.
	if k.HasPendingRemoval(ctx, msg.FxId) {
		return nil, errors.Wrapf(types.ErrRemovalInProgress, "fx_id %s is mid-removal", msg.FxId)
	}
	if k.HasFxRequest(ctx, msg.FxId) {
		return nil, errors.Wrapf(types.ErrFxOnboarding, "fx_id %s already has a pending request", msg.FxId)
	}
	guarantor, found := k.GetInstitution(ctx, msg.GuarantorId)
	if !found || !k.isActiveFinancialGuarantor(guarantor) {
		return nil, errors.Wrapf(types.ErrGuarantorRequired, "guarantor_id %s is not an active financial institution", msg.GuarantorId)
	}

	k.SetFxRequest(ctx, types.FxEntryRequest{
		FxId:        msg.FxId,
		Applicant:   msg.Applicant,
		License:     msg.License,
		GuarantorId: msg.GuarantorId,
		Status:      types.FxEntryStatus_FX_ENTRY_REQUESTED,
	})

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeFxEntryRequested,
		sdk.NewAttribute(types.AttributeKeyFxID, msg.FxId),
		sdk.NewAttribute(types.AttributeKeyApplicant, msg.Applicant),
		sdk.NewAttribute(types.AttributeKeyGuarantor, msg.GuarantorId),
	))
	return &types.MsgRequestFxEntryResponse{}, nil
}

// GuaranteeFxEntry: the named financial institution's admin approves or declines the guarantee.
func (k msgServer) GuaranteeFxEntry(goCtx context.Context, msg *types.MsgGuaranteeFxEntry) (*types.MsgGuaranteeFxEntryResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	req, found := k.GetFxRequest(ctx, msg.FxId)
	if !found {
		return nil, errors.Wrapf(types.ErrFxOnboarding, "no request for fx_id %s", msg.FxId)
	}
	if req.Status != types.FxEntryStatus_FX_ENTRY_REQUESTED {
		return nil, errors.Wrapf(types.ErrFxOnboarding, "request for fx_id %s is not awaiting a guarantee (status %s)", msg.FxId, req.Status)
	}
	guarantor, found := k.GetInstitution(ctx, req.GuarantorId)
	if !found || !k.isActiveFinancialGuarantor(guarantor) {
		return nil, errors.Wrapf(types.ErrGuarantorRequired, "guarantor %s is no longer an active financial institution", req.GuarantorId)
	}
	if err := k.requireRole(ctx, guarantor, msg.GuarantorAdmin, types.INSTITUTION_ROLE_ADMIN, types.INSTITUTION_ROLE_OPERATOR); err != nil {
		return nil, err
	}

	if !msg.Approve {
		// Decline clears the request so the applicant may re-apply.
		k.DeleteFxRequest(ctx, msg.FxId)
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeFxEntryDeclined,
			sdk.NewAttribute(types.AttributeKeyFxID, msg.FxId),
			sdk.NewAttribute(types.AttributeKeyGuarantor, req.GuarantorId),
		))
		return &types.MsgGuaranteeFxEntryResponse{}, nil
	}

	req.Status = types.FxEntryStatus_FX_ENTRY_GUARANTEED
	k.SetFxRequest(ctx, req)
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeFxEntryGuaranteed,
		sdk.NewAttribute(types.AttributeKeyFxID, msg.FxId),
		sdk.NewAttribute(types.AttributeKeyGuarantor, req.GuarantorId),
	))
	return &types.MsgGuaranteeFxEntryResponse{}, nil
}

// FinalizeFxEntry: finalizes onboarding after a passed public vote (or direct operator add during bootstrap).
func (k msgServer) FinalizeFxEntry(goCtx context.Context, msg *types.MsgFinalizeFxEntry) (*types.MsgFinalizeFxEntryResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Only the operator or the governance authority may finalize.
	params := k.GetParams(ctx)
	isOperator := params.Operator != "" && msg.Operator == params.Operator
	isAuthority := msg.Operator == k.authority
	if !isOperator && !isAuthority {
		return nil, errors.Wrap(types.ErrUnauthorized, "only the operator or governance may finalize fx onboarding")
	}
	req, found := k.GetFxRequest(ctx, msg.FxId)
	if !found {
		return nil, errors.Wrapf(types.ErrFxOnboarding, "no request for fx_id %s", msg.FxId)
	}
	if req.Status != types.FxEntryStatus_FX_ENTRY_GUARANTEED {
		return nil, errors.Wrapf(types.ErrFxOnboarding, "request for fx_id %s is not guaranteed yet (status %s)", msg.FxId, req.Status)
	}
	if k.HasInstitution(ctx, msg.FxId) {
		return nil, errors.Wrapf(types.ErrInstitutionExists, "id %s already registered", msg.FxId)
	}
	// A mid-removal id must fully drain before re-creation here (as at RegisterInstitution).
	if k.HasPendingRemoval(ctx, msg.FxId) {
		return nil, errors.Wrapf(types.ErrRemovalInProgress, "fx_id %s is mid-removal", msg.FxId)
	}

	// Bootstrap: operator may add directly; afterwards requires a PASSED public proposal (fail-closed).
	bootstrap := k.identityKeeper.BootstrapPhase(ctx)
	if !bootstrap && !isAuthority {
		if err := k.requirePassedProposalFor(ctx, msg.ProposalId, req.FxId); err != nil {
			return nil, err
		}
	}

	inst := types.Institution{
		Id:              req.FxId,
		License:         req.License,
		Admin:           req.Applicant,
		VaultAccount:    "",
		VaultApi:        "",
		Bond:            "0",
		Status:          types.INSTITUTION_STATUS_HEALTHY,
		VaultBalance:    "0",
		AttestedReserve: "0",
		PausedMint:      false,
		InstitutionType: types.INSTITUTION_TYPE_FX,
		// §4.6 attestation clock starts at onboarding, not the epoch.
		LastAttestedAt: ctx.BlockTime().Unix(),
	}
	k.SetInstitution(ctx, inst)
	k.DeleteFxRequest(ctx, req.FxId)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeFxEntryFinalized,
		sdk.NewAttribute(types.AttributeKeyFxID, req.FxId),
		sdk.NewAttribute(types.AttributeKeyAdmin, req.Applicant),
		sdk.NewAttribute(types.AttributeKeyProposalID, fmt.Sprintf("%d", msg.ProposalId)),
	))
	return &types.MsgFinalizeFxEntryResponse{}, nil
}

func (k Keeper) requirePassedProposalFor(ctx sdk.Context, proposalID uint64, fxID string) error {
	if proposalID == 0 {
		return errors.Wrap(types.ErrFxOnboarding, "a passed proposal_id is required outside the bootstrap phase")
	}
	if k.govKeeper == nil {
		return errors.Wrap(types.ErrFxOnboarding, "governance not available for fx finalization")
	}
	prop, err := k.govKeeper.Proposal(ctx, proposalID)
	if err != nil {
		return errors.Wrapf(types.ErrFxOnboarding, "proposal %d not found", proposalID)
	}
	if prop.Status != govv1.ProposalStatus_PROPOSAL_STATUS_PASSED {
		return errors.Wrapf(types.ErrFxOnboarding, "proposal %d has not passed (status %s)", proposalID, prop.Status)
	}
	// The proposal must contain a MsgFinalizeFxEntry naming this fx_id.
	target := sdk.MsgTypeURL(&types.MsgFinalizeFxEntry{})
	for _, anyMsg := range prop.Messages {
		if anyMsg == nil || anyMsg.TypeUrl != target {
			continue
		}
		var fm types.MsgFinalizeFxEntry
		if err := k.cdc.Unmarshal(anyMsg.Value, &fm); err != nil {
			continue
		}
		if fm.FxId == fxID {
			return nil
		}
	}
	return errors.Wrapf(types.ErrFxOnboarding,
		"proposal %d does not authorize onboarding fx_id %q (no matching MsgFinalizeFxEntry)", proposalID, fxID)
}
