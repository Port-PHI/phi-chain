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

// fx onboarding (bootstrap): a licensed currency exchange joins the network in three steps —
// (1) it applies, naming an active financial institution as guarantor; (2) the guarantor approves;
// (3) onboarding is finalized, either directly by the operator during the bootstrap phase or against
// a PASSED public (one-human-one-vote) x/gov proposal. On finalize the exchange becomes an
// INSTITUTION_TYPE_FX institution with its own Rial vault (the solvency invariant is unchanged).

// --- FxEntryRequest storage ---

// SetFxRequest stores a pending fx onboarding request keyed by fx_id.
func (k Keeper) SetFxRequest(ctx sdk.Context, req types.FxEntryRequest) {
	ctx.KVStore(k.storeKey).Set(types.FxRequestKey(req.FxId), k.cdc.MustMarshal(&req))
}

// GetFxRequest returns a pending request by fx_id.
func (k Keeper) GetFxRequest(ctx sdk.Context, fxID string) (types.FxEntryRequest, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.FxRequestKey(fxID))
	if bz == nil {
		return types.FxEntryRequest{}, false
	}
	var req types.FxEntryRequest
	k.cdc.MustUnmarshal(bz, &req)
	return req, true
}

// HasFxRequest reports whether a pending request exists for the fx_id.
func (k Keeper) HasFxRequest(ctx sdk.Context, fxID string) bool {
	return ctx.KVStore(k.storeKey).Has(types.FxRequestKey(fxID))
}

// DeleteFxRequest removes a request (decline or finalize).
func (k Keeper) DeleteFxRequest(ctx sdk.Context, fxID string) {
	ctx.KVStore(k.storeKey).Delete(types.FxRequestKey(fxID))
}

// IterateFxRequests iterates over all pending requests (genesis export); returning true stops.
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

// isActiveFinancialGuarantor reports whether the institution is a present, non-frozen financial
// institution eligible to guarantee an fx applicant (an exchange cannot guarantee another exchange).
func (k Keeper) isActiveFinancialGuarantor(inst types.Institution) bool {
	isFinancial := inst.InstitutionType == types.INSTITUTION_TYPE_FINANCIAL ||
		inst.InstitutionType == types.INSTITUTION_TYPE_UNSPECIFIED // pre-existing institutions default to financial
	return isFinancial && inst.Status != types.INSTITUTION_STATUS_FROZEN
}

// --- Msg handlers ---

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
	if k.HasFxRequest(ctx, msg.FxId) {
		return nil, errors.Wrapf(types.ErrFxOnboarding, "fx_id %s already has a pending request", msg.FxId)
	}
	// The guarantor must be a present, active financial institution at request time.
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
	// Only the guarantor institution's admin/operator may sign the guarantee.
	if err := k.requireRole(ctx, guarantor, msg.GuarantorAdmin, types.INSTITUTION_ROLE_ADMIN, types.INSTITUTION_ROLE_OPERATOR); err != nil {
		return nil, err
	}

	if !msg.Approve {
		// Decline clears the request so the applicant may re-apply (possibly with another guarantor).
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

	// The operator (registry executor) or the governance authority may submit a finalize.
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

	// Onboarding authority: during the bootstrap phase the operator may add directly. Afterwards the
	// authority is a PASSED public (one-human-one-vote) proposal that the operator merely executes
	// (fail-closed if the gov keeper is unwired). A governance-authority finalize is itself a passed
	// on-chain decision, so it needs no separate proposal reference.
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
	}
	k.SetInstitution(ctx, inst)
	// The request is consumed: the institution record is now the source of truth.
	k.DeleteFxRequest(ctx, req.FxId)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeFxEntryFinalized,
		sdk.NewAttribute(types.AttributeKeyFxID, req.FxId),
		sdk.NewAttribute(types.AttributeKeyAdmin, req.Applicant),
		sdk.NewAttribute(types.AttributeKeyProposalID, fmt.Sprintf("%d", msg.ProposalId)),
	))
	return &types.MsgFinalizeFxEntryResponse{}, nil
}

// requirePassedProposalFor verifies that proposalID references an existing, PASSED x/gov proposal
// that is bound to this fx_id — it must carry a MsgFinalizeFxEntry naming exactly fxID. Without
// the binding, any unrelated passed proposal (e.g. a parameter change) could authorize onboarding an
// arbitrary exchange. Fail-closed: a missing gov keeper, lookup error, wrong status, or missing
// binding rejects the finalize.
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
	// The proposal must contain a MsgFinalizeFxEntry naming this fx_id. Read the Any's type URL and
	// bytes directly (no dependency on the cached unpacked value being populated).
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
