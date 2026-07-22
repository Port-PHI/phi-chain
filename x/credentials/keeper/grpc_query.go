// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"context"
	"encoding/hex"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Port-PHI/phi-chain/x/credentials/types"
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

// Template returns a credential template by id.
func (k Keeper) Template(goCtx context.Context, req *types.QueryTemplateRequest) (*types.QueryTemplateResponse, error) {
	if req == nil || req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id cannot be empty")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	t, found := k.GetTemplate(ctx, req.Id)
	if !found {
		return nil, status.Errorf(codes.NotFound, "template %s not found", req.Id)
	}
	return &types.QueryTemplateResponse{Template: t}, nil
}

// Credential returns a credential anchor by hex-encoded hash.
func (k Keeper) Credential(goCtx context.Context, req *types.QueryCredentialRequest) (*types.QueryCredentialResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	hash, err := hex.DecodeString(req.CredentialHashHex)
	if err != nil || len(hash) == 0 {
		return nil, status.Error(codes.InvalidArgument, "credential_hash_hex must be non-empty hex")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	a, found := k.GetAnchor(ctx, hash)
	if !found {
		return nil, status.Errorf(codes.NotFound, "credential %s not found", req.CredentialHashHex)
	}
	return &types.QueryCredentialResponse{Anchor: a}, nil
}

// Agreement returns an agreement by hex-encoded hash.
func (k Keeper) Agreement(goCtx context.Context, req *types.QueryAgreementRequest) (*types.QueryAgreementResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	hash, err := hex.DecodeString(req.HashHex)
	if err != nil || len(hash) == 0 {
		return nil, status.Error(codes.InvalidArgument, "hash_hex must be non-empty hex")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	ag, found := k.GetAgreement(ctx, hash)
	if !found {
		return nil, status.Errorf(codes.NotFound, "agreement %s not found", req.HashHex)
	}
	return &types.QueryAgreementResponse{Agreement: ag}, nil
}

// PersonalAnchor returns a personal anchor by owner DID and hex-encoded hash.
func (k Keeper) PersonalAnchor(goCtx context.Context, req *types.QueryPersonalAnchorRequest) (*types.QueryPersonalAnchorResponse, error) {
	if req == nil || req.OwnerDid == "" {
		return nil, status.Error(codes.InvalidArgument, "owner_did cannot be empty")
	}
	hash, err := hex.DecodeString(req.AnchorHashHex)
	if err != nil || len(hash) == 0 {
		return nil, status.Error(codes.InvalidArgument, "anchor_hash_hex must be non-empty hex")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	p, found := k.GetPersonalAnchor(ctx, req.OwnerDid, hash)
	if !found {
		return nil, status.Errorf(codes.NotFound, "personal anchor not found")
	}
	return &types.QueryPersonalAnchorResponse{Anchor: p}, nil
}
