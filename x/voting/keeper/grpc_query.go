// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"context"
	"encoding/hex"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Port-PHI/phi-chain/x/voting/types"
)

var _ types.QueryServer = Keeper{}

// Params returns the current module parameters.
func (k Keeper) Params(goCtx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	return &types.QueryParamsResponse{Params: k.GetParams(ctx)}, nil
}

// Election returns an election by id, including its running public tally.
func (k Keeper) Election(goCtx context.Context, req *types.QueryElectionRequest) (*types.QueryElectionResponse, error) {
	if req == nil || req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id cannot be empty")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	e, found := k.GetElection(ctx, req.Id)
	if !found {
		return nil, status.Errorf(codes.NotFound, "election %s not found", req.Id)
	}
	return &types.QueryElectionResponse{Election: e}, nil
}

// Voted reports whether a nullifier (hex) has already voted in an election.
func (k Keeper) Voted(goCtx context.Context, req *types.QueryVotedRequest) (*types.QueryVotedResponse, error) {
	if req == nil || req.ElectionId == "" {
		return nil, status.Error(codes.InvalidArgument, "election_id cannot be empty")
	}
	nullifier, err := hex.DecodeString(req.NullifierHex)
	if err != nil || len(nullifier) == 0 {
		return nil, status.Error(codes.InvalidArgument, "nullifier_hex must be non-empty hex")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	return &types.QueryVotedResponse{Voted: k.HasBallot(ctx, req.ElectionId, nullifier)}, nil
}
