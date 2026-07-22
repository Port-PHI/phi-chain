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

// CoinAge returns an address's FIFO coin-age lot queue (oldest first).
func (k Keeper) CoinAge(goCtx context.Context, req *types.QueryCoinAgeRequest) (*types.QueryCoinAgeResponse, error) {
	if req == nil || req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address cannot be empty")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	return &types.QueryCoinAgeResponse{CoinAge: k.GetCoinAge(ctx, req.Address)}, nil
}
