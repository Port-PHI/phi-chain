// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Port-PHI/phi-chain/x/coin/types"
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

// CoinAge returns an address's coin-age buckets (after maturation).
func (k Keeper) CoinAge(goCtx context.Context, req *types.QueryCoinAgeRequest) (*types.QueryCoinAgeResponse, error) {
	if req == nil || req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address cannot be empty")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	ca := MatureCoinAge(k.GetCoinAge(ctx, req.Address), ctx.BlockTime().Unix(), k.GetParams(ctx).CoinAgeThresholdSeconds)
	return &types.QueryCoinAgeResponse{CoinAge: ca}, nil
}
