// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"bytes"
	"context"
	"fmt"

	"cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/phicrypto"
	"github.com/Port-PHI/phi-chain/x/identity/types"
)

// Recovery rotates a DID's key when the current key is gone.

// InitiateRecovery opens a recovery request, signed by the NEW key's account (locked in as controller-to-be).
func (k msgServer) InitiateRecovery(goCtx context.Context, msg *types.MsgInitiateRecovery) (*types.MsgInitiateRecoveryResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	params := k.GetParams(ctx)
	reauth := msg.Method == types.RECOVERY_METHOD_REAUTH

	// fail-closed: REAUTH is compiled inert in the default binary (build-tag switch, not governance)
	if reauth && !ReauthRecoveryEnabled {
		return nil, errors.Wrap(types.ErrReauthNotEnabled, "method REAUTH")
	}

	doc, found := k.GetIdentity(ctx, msg.Did)
	if !found {
		return nil, errors.Wrapf(types.ErrIdentityNotFound, "did %s", msg.Did)
	}
	// only an ACTIVE identity is recoverable (SUSPENDED is a freeze, REVOKED is terminal)
	if doc.Status != types.DID_STATUS_ACTIVE {
		return nil, errors.Wrapf(types.ErrInvalidRecovery, "did %s is not ACTIVE", msg.Did)
	}
	var gs types.GuardianSet
	if !reauth {
		gs, found = k.GetGuardians(ctx, msg.Did)
		if !found {
			return nil, errors.Wrapf(types.ErrInvalidGuardians, "did %s has no guardian set", msg.Did)
		}
	}
	// no-op key replacement would still cost a slot
	if bytes.Equal(msg.ProposedNewPubKey, doc.PubKey) {
		return nil, errors.Wrap(types.ErrInvalidRecovery, "proposed_new_pub_key is already this DID's key")
	}
	// key-reuse hazard: proposed key must not already self-certify another DID
	if err := k.assertNoKeyCollision(ctx, msg.KeyType, msg.ProposedNewPubKey); err != nil {
		return nil, err
	}
	// anti-replay: single-use per-DID nonce, shared by both methods
	if k.hasRecoveryNonce(ctx, msg.Did, msg.Nonce) {
		return nil, errors.Wrapf(types.ErrRecoveryNonceReused, "did %s", msg.Did)
	}
	// fail-closed PoP of the NEW key, bound to DID+controller+nonce; required for both methods
	popMsg := types.SocialRecoveryPoPMessage(ctx.ChainID(), msg.Did, msg.ProposedNewPubKey, msg.Creator, msg.Nonce)
	if !k.verifier.VerifySignature(phicrypto.Secp256r1, msg.ProposedNewPubKey, popMsg, msg.PopSig) {
		return nil, errors.Wrap(types.ErrInvalidPoP, "new-key proof-of-possession did not verify")
	}
	// REAUTH authorisation: issuer signature binding the DID's OWN uniqueness marker (read from doc, never msg — the anti-Sybil crux), key, controller and nonce
	if reauth {
		if err := k.verifyReauthAttestation(ctx, msg, doc); err != nil {
			return nil, err
		}
	}

	// slot cap over live requests (expired reaped first); the deposit prices griefing
	open, err := k.countOpenRequests(ctx, msg.Did)
	if err != nil {
		return nil, err
	}
	if open >= params.MaxOpenRecoveryRequests {
		return nil, errors.Wrapf(types.ErrRecoverySlotsFull,
			"did %s already has %d open requests (max %d)", msg.Did, open, params.MaxOpenRecoveryRequests)
	}

	recoveryID := types.DeriveRecoveryID(msg.Did, msg.ProposedNewPubKey, msg.Nonce)
	if _, exists := k.GetRecoveryRequest(ctx, recoveryID); exists {
		return nil, errors.Wrap(types.ErrInvalidRecovery, "a request with this id already exists")
	}

	// charge before writing the request.
	initiator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, err
	}
	deposit, fee := math.ZeroInt(), math.ZeroInt()
	if reauth {
		fee = params.ReauthRecoveryFee()
		if err := k.chargeReauthFee(ctx, initiator, fee); err != nil {
			return nil, errors.Wrap(err, "charge reauth recovery fee")
		}
	} else {
		deposit = params.RecoveryDeposit()
		if err := k.escrowDeposit(ctx, initiator, deposit); err != nil {
			return nil, errors.Wrap(err, "escrow recovery deposit")
		}
	}

	now := ctx.BlockTime().Unix()
	req := types.RecoveryRequest{
		RecoveryId:            recoveryID,
		Did:                   msg.Did,
		ProposedNewPubKey:     msg.ProposedNewPubKey,
		ProposedNewController: msg.Creator,
		KeyType:               msg.KeyType,
		Method:                msg.Method,
		Approvals:             []string{},
		Nonce:                 msg.Nonce,
		InitiatedAt:           now,
		ExecuteAfter:          now + params.RecoveryDelaySeconds,
		ExpiresAt:             now + params.RecoveryRequestTtlSeconds,
		DepositUphi:           deposit.String(),
		AttestorDid:           msg.AttestorDid,
		FeeUphi:               fee.String(),
		Status:                types.RECOVERY_STATUS_PENDING,
	}
	k.SetRecoveryRequest(ctx, req)
	k.markRecoveryNonce(ctx, msg.Did, msg.Nonce)

	// public opposition notice: tells the true owner they have until execute_after to veto
	attrs := []sdk.Attribute{
		sdk.NewAttribute(types.AttributeKeyRecoveryID, hexID(recoveryID)),
		sdk.NewAttribute(types.AttributeKeyDID, msg.Did),
		sdk.NewAttribute(types.AttributeKeyMethod, msg.Method.String()),
		sdk.NewAttribute(types.AttributeKeyNewController, msg.Creator),
		sdk.NewAttribute(types.AttributeKeyExecuteAfter, fmt.Sprintf("%d", req.ExecuteAfter)),
	}
	if reauth {
		attrs = append(attrs,
			sdk.NewAttribute(types.AttributeKeyAttestorDID, msg.AttestorDid),
			sdk.NewAttribute(types.AttributeKeyFee, req.FeeUphi),
		)
	} else {
		attrs = append(attrs,
			sdk.NewAttribute(types.AttributeKeyThreshold, fmt.Sprintf("%d", gs.Threshold)),
			sdk.NewAttribute(types.AttributeKeyDeposit, req.DepositUphi),
		)
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeRecoveryInitiated, attrs...))
	return &types.MsgInitiateRecoveryResponse{RecoveryId: recoveryID}, nil
}

// ApproveRecovery records one guardian's approval, enforcing every guardian rule against live state.
func (k msgServer) ApproveRecovery(goCtx context.Context, msg *types.MsgApproveRecovery) (*types.MsgApproveRecoveryResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	req, found := k.GetRecoveryRequest(ctx, msg.RecoveryId)
	if !found {
		return nil, errors.Wrap(types.ErrRecoveryNotFound, hexID(msg.RecoveryId))
	}
	terminal, err := k.reapIfExpired(ctx, &req)
	if err != nil {
		return nil, err
	}
	if terminal {
		return nil, errors.Wrapf(types.ErrRecoveryNotPending, "status %s", req.Status)
	}
	// REAUTH takes no guardian approvals — the attestation is the authorisation
	if req.Method == types.RECOVERY_METHOD_REAUTH {
		return nil, errors.Wrap(types.ErrInvalidRecovery, "a REAUTH recovery takes no guardian approvals")
	}

	if err := k.openGuardianCommitment(ctx, req.Did, msg.GuardianDid, msg.Salt, msg.Creator); err != nil {
		return nil, err
	}

	// discard a tally collected under a since-replaced guardian set
	k.syncTallyEpoch(ctx, &req)

	// dedup by REVEALED DID, not commitment: one guardian may be enrolled twice under two salts
	for _, a := range req.Approvals {
		if a == msg.GuardianDid {
			return nil, errors.Wrapf(types.ErrAlreadyApproved, "guardian %s", msg.GuardianDid)
		}
	}
	req.Approvals = append(req.Approvals, msg.GuardianDid)
	k.SetRecoveryRequest(ctx, req)

	gs, _ := k.GetGuardians(ctx, req.Did)
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRecoveryApproved,
		sdk.NewAttribute(types.AttributeKeyRecoveryID, hexID(req.RecoveryId)),
		sdk.NewAttribute(types.AttributeKeyDID, req.Did),
		sdk.NewAttribute(types.AttributeKeyGuardianDID, msg.GuardianDid),
		sdk.NewAttribute(types.AttributeKeyApprovals, fmt.Sprintf("%d", len(req.Approvals))),
		sdk.NewAttribute(types.AttributeKeyThreshold, fmt.Sprintf("%d", gs.Threshold)),
	))
	return &types.MsgApproveRecoveryResponse{}, nil
}

// RejectRecovery records one guardian's rejection of an open SOCIAL request, closing it at the guardian THRESHOLD.
func (k msgServer) RejectRecovery(goCtx context.Context, msg *types.MsgRejectRecovery) (*types.MsgRejectRecoveryResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	req, found := k.GetRecoveryRequest(ctx, msg.RecoveryId)
	if !found {
		return nil, errors.Wrap(types.ErrRecoveryNotFound, hexID(msg.RecoveryId))
	}
	terminal, err := k.reapIfExpired(ctx, &req)
	if err != nil {
		return nil, err
	}
	if terminal {
		return nil, errors.Wrapf(types.ErrRecoveryNotPending, "status %s", req.Status)
	}
	// REAUTH takes no guardian rejections — mirroring the approval guard
	if req.Method == types.RECOVERY_METHOD_REAUTH {
		return nil, errors.Wrap(types.ErrInvalidRecovery, "a REAUTH recovery takes no guardian rejections")
	}

	if err := k.openGuardianCommitment(ctx, req.Did, msg.GuardianDid, msg.Salt, msg.Creator); err != nil {
		return nil, err
	}

	// discard a tally collected under a since-replaced guardian set
	k.syncTallyEpoch(ctx, &req)

	// dedup by REVEALED DID, not commitment: one guardian may be enrolled twice under two salts
	for _, r := range req.Rejections {
		if r == msg.GuardianDid {
			return nil, errors.Wrapf(types.ErrAlreadyRejected, "guardian %s", msg.GuardianDid)
		}
	}
	req.Rejections = append(req.Rejections, msg.GuardianDid)

	gs, found := k.GetGuardians(ctx, req.Did)
	if !found {
		return nil, errors.Wrapf(types.ErrInvalidGuardians, "did %s has no guardian set", req.Did)
	}
	// at threshold: forfeit deposit (moved to fee collector, never burned — solvency) and delete
	reached := uint64(len(req.Rejections)) >= uint64(gs.Threshold)
	if reached {
		if err := k.forfeitDeposit(ctx, req); err != nil {
			return nil, errors.Wrap(err, "forfeit recovery deposit")
		}
		req.Status = types.RECOVERY_STATUS_REJECTED
		k.deleteRecoveryRequest(ctx, req)
	} else {
		k.SetRecoveryRequest(ctx, req)
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRecoveryRejected,
		sdk.NewAttribute(types.AttributeKeyRecoveryID, hexID(req.RecoveryId)),
		sdk.NewAttribute(types.AttributeKeyDID, req.Did),
		sdk.NewAttribute(types.AttributeKeyGuardianDID, msg.GuardianDid),
		sdk.NewAttribute(types.AttributeKeyRejections, fmt.Sprintf("%d", len(req.Rejections))),
		sdk.NewAttribute(types.AttributeKeyThreshold, fmt.Sprintf("%d", gs.Threshold)),
	))
	return &types.MsgRejectRecoveryResponse{}, nil
}

// ExecuteRecovery applies an authorised recovery.
func (k msgServer) ExecuteRecovery(goCtx context.Context, msg *types.MsgExecuteRecovery) (*types.MsgExecuteRecoveryResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	req, found := k.GetRecoveryRequest(ctx, msg.RecoveryId)
	if !found {
		return nil, errors.Wrap(types.ErrRecoveryNotFound, hexID(msg.RecoveryId))
	}
	terminal, err := k.reapIfExpired(ctx, &req)
	if err != nil {
		return nil, err
	}
	if terminal {
		return nil, errors.Wrapf(types.ErrRecoveryNotPending, "status %s", req.Status)
	}
	// anti-hijack delay: the opposition window must have fully elapsed
	if ctx.BlockTime().Unix() < req.ExecuteAfter {
		return nil, errors.Wrapf(types.ErrRecoveryTooEarly,
			"execute_after %d, now %d", req.ExecuteAfter, ctx.BlockTime().Unix())
	}
	// authorisation gate — the only per-method difference at execute
	if req.Method != types.RECOVERY_METHOD_REAUTH {
		gs, found := k.GetGuardians(ctx, req.Did)
		if !found {
			return nil, errors.Wrapf(types.ErrInvalidGuardians, "did %s has no guardian set", req.Did)
		}
		// only approvals under the currently-in-force set count, so a rotation withdraws rotated-out consent
		approvals := k.EffectiveApprovals(ctx, req)
		if uint64(len(approvals)) < uint64(gs.Threshold) {
			return nil, errors.Wrapf(types.ErrRecoveryBelowQuorum,
				"%d approvals, threshold %d", len(approvals), gs.Threshold)
		}
	}

	doc, found := k.GetIdentity(ctx, req.Did)
	if !found {
		return nil, errors.Wrapf(types.ErrIdentityNotFound, "did %s", req.Did)
	}
	// DID must STILL be ACTIVE (as at initiate); this also keeps the owner's cancel-veto reachable, since the ante refuses transactions from a suspended controller
	if doc.Status != types.DID_STATUS_ACTIVE {
		return nil, errors.Wrapf(types.ErrInvalidRecovery, "did %s is not ACTIVE", req.Did)
	}
	// re-check collision at execute: the registry may have changed during the window
	if err := k.assertNoKeyCollision(ctx, req.KeyType, req.ProposedNewPubKey); err != nil {
		return nil, err
	}

	// rotation: only pub_key, key_type and controller change; did/uniqueness_hash/created_at/status are preserved (keeps the DID unique and its voting age).
	doc.PubKey = req.ProposedNewPubKey
	doc.KeyType = req.KeyType
	doc.Controller = req.ProposedNewController
	k.SetIdentity(ctx, doc)

	if err := k.refundDeposit(ctx, req); err != nil {
		return nil, errors.Wrap(err, "refund recovery deposit")
	}
	req.Status = types.RECOVERY_STATUS_EXECUTED
	k.deleteRecoveryRequest(ctx, req)

	// sibling requests were authorised against a key that no longer exists
	if err := k.supersedeSiblings(ctx, req.Did, req.RecoveryId); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRecoveryExecuted,
		sdk.NewAttribute(types.AttributeKeyRecoveryID, hexID(req.RecoveryId)),
		sdk.NewAttribute(types.AttributeKeyDID, req.Did),
		sdk.NewAttribute(types.AttributeKeyNewController, req.ProposedNewController),
	))
	return &types.MsgExecuteRecoveryResponse{}, nil
}

// CancelRecovery aborts a pending recovery.
func (k msgServer) CancelRecovery(goCtx context.Context, msg *types.MsgCancelRecovery) (*types.MsgCancelRecoveryResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	req, found := k.GetRecoveryRequest(ctx, msg.RecoveryId)
	if !found {
		return nil, errors.Wrap(types.ErrRecoveryNotFound, hexID(msg.RecoveryId))
	}
	terminal, err := k.reapIfExpired(ctx, &req)
	if err != nil {
		return nil, err
	}
	if terminal {
		return nil, errors.Wrapf(types.ErrRecoveryNotPending, "status %s", req.Status)
	}
	doc, found := k.GetIdentity(ctx, req.Did)
	if !found {
		return nil, errors.Wrapf(types.ErrIdentityNotFound, "did %s", req.Did)
	}
	if msg.Creator != doc.Controller {
		return nil, errors.Wrap(types.ErrUnauthorized, "only the current controller may cancel a recovery")
	}

	if err := k.forfeitDeposit(ctx, req); err != nil {
		return nil, errors.Wrap(err, "forfeit recovery deposit")
	}
	req.Status = types.RECOVERY_STATUS_REJECTED
	k.deleteRecoveryRequest(ctx, req)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRecoveryCancelled,
		sdk.NewAttribute(types.AttributeKeyRecoveryID, hexID(req.RecoveryId)),
		sdk.NewAttribute(types.AttributeKeyDID, req.Did),
	))
	return &types.MsgCancelRecoveryResponse{}, nil
}
