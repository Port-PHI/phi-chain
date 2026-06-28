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

// IdentityCount returns the total identity counter.
func (k Keeper) IdentityCount(goCtx context.Context, req *types.QueryIdentityCountRequest) (*types.QueryIdentityCountResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	return &types.QueryIdentityCountResponse{Count: k.GetIdentityCount(ctx)}, nil
}
