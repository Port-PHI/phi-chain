// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

var _ types.QueryServer = Keeper{}

// Params returns the current parameters.
func (k Keeper) Params(goCtx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	return &types.QueryParamsResponse{Params: k.GetParams(ctx)}, nil
}

// Identity returns a DIDDocument by its DID.
func (k Keeper) Identity(goCtx context.Context, req *types.QueryIdentityRequest) (*types.QueryIdentityResponse, error) {
	if req == nil || req.Did == "" {
		return nil, status.Error(codes.InvalidArgument, "did cannot be empty")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	doc, found := k.GetIdentity(ctx, req.Did)
	if !found {
		return nil, status.Errorf(codes.NotFound, "identity %s not found", req.Did)
	}
	return &types.QueryIdentityResponse{Identity: doc}, nil
}

// Guardians returns a DID's guardian config shape (count and threshold) only — never the commitments or any guardian identity, which the commitment scheme keeps unreadable.
func (k Keeper) Guardians(goCtx context.Context, req *types.QueryGuardiansRequest) (*types.QueryGuardiansResponse, error) {
	if req == nil || req.Did == "" {
		return nil, status.Error(codes.InvalidArgument, "did cannot be empty")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	gs, found := k.GetGuardians(ctx, req.Did)
	if !found {
		return nil, status.Errorf(codes.NotFound, "no guardian set for %s", req.Did)
	}
	return &types.QueryGuardiansResponse{
		CommitmentCount: uint32(len(gs.Commitments)),
		Threshold:       gs.Threshold,
		UpdatedAt:       gs.UpdatedAt,
	}, nil
}

// RecoveryRequest returns one recovery request by id.
func (k Keeper) RecoveryRequest(goCtx context.Context, req *types.QueryRecoveryRequestRequest) (*types.QueryRecoveryRequestResponse, error) {
	if req == nil || len(req.RecoveryId) == 0 {
		return nil, status.Error(codes.InvalidArgument, "recovery_id cannot be empty")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	r, found := k.GetRecoveryRequest(ctx, req.RecoveryId)
	if !found {
		return nil, status.Error(codes.NotFound, "recovery request not found")
	}
	return &types.QueryRecoveryRequestResponse{Request: r}, nil
}

// RecoveryRequestsByDID returns every recovery request recorded for a DID (open and terminal).
func (k Keeper) RecoveryRequestsByDID(goCtx context.Context, req *types.QueryRecoveryRequestsByDIDRequest) (*types.QueryRecoveryRequestsByDIDResponse, error) {
	if req == nil || req.Did == "" {
		return nil, status.Error(codes.InvalidArgument, "did cannot be empty")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	return &types.QueryRecoveryRequestsByDIDResponse{Requests: k.RecoveryRequestsForDID(ctx, req.Did)}, nil
}

// IdentityCount returns the total identity counter.
func (k Keeper) IdentityCount(goCtx context.Context, req *types.QueryIdentityCountRequest) (*types.QueryIdentityCountResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	return &types.QueryIdentityCountResponse{Count: k.GetIdentityCount(ctx)}, nil
}
