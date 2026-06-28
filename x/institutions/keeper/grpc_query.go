// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
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

// Institution returns an institution by id.
func (k Keeper) Institution(goCtx context.Context, req *types.QueryInstitutionRequest) (*types.QueryInstitutionResponse, error) {
	if req == nil || req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id cannot be empty")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	inst, found := k.GetInstitution(ctx, req.Id)
	if !found {
		return nil, status.Errorf(codes.NotFound, "institution %s not found", req.Id)
	}
	return &types.QueryInstitutionResponse{Institution: inst}, nil
}

// Institutions lists all institutions.
func (k Keeper) Institutions(goCtx context.Context, req *types.QueryInstitutionsRequest) (*types.QueryInstitutionsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	out := []types.Institution{}
	k.IterateInstitutions(ctx, func(inst types.Institution) bool {
		out = append(out, inst)
		return false
	})
	return &types.QueryInstitutionsResponse{Institutions: out}, nil
}

// Solvency returns the global solvency summary.
func (k Keeper) Solvency(goCtx context.Context, req *types.QuerySolvencyRequest) (*types.QuerySolvencyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	_, broken := SolvencyInvariant(k)(ctx)
	return &types.QuerySolvencyResponse{
		TotalSupplyUphi:      k.TotalSupplyUphi(ctx).String(),
		SumVaultBalanceToman: k.SumVaultBalance(ctx).String(),
		Solvent:              !broken,
	}, nil
}
