// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	credentialstypes "github.com/Port-PHI/phi-chain/x/credentials/types"
	"github.com/Port-PHI/phi-chain/x/disclosure/types"
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

// VerifyDisclosure authoritatively verifies a BBS+ selective-disclosure proof
// against an anchored, non-revoked credential. It is a query (read-only RPC):
// verification is stateless, the proof is ephemeral, and nothing is persisted.
// It returns a boolean plus issuer/template context. A false result carries a
// reason; only malformed input yields a gRPC error.
func (k Keeper) VerifyDisclosure(goCtx context.Context, req *types.QueryVerifyDisclosureRequest) (*types.QueryVerifyDisclosureResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if len(req.CredentialHash) == 0 {
		return nil, status.Error(codes.InvalidArgument, "credential_hash cannot be empty")
	}
	if len(req.Proof) == 0 {
		return nil, status.Error(codes.InvalidArgument, "proof cannot be empty")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)

	if uint32(len(req.Proof)) > k.GetParams(ctx).MaxProofSizeBytes {
		return &types.QueryVerifyDisclosureResponse{Valid: false, Reason: "proof exceeds max_proof_size_bytes"}, nil
	}

	anchor, ok := k.credentialsKeeper.GetAnchor(ctx, req.CredentialHash)
	if !ok {
		return &types.QueryVerifyDisclosureResponse{Valid: false, Reason: "credential not anchored"}, nil
	}
	resp := &types.QueryVerifyDisclosureResponse{IssuerDid: anchor.IssuerDid, TemplateId: anchor.TemplateId}
	if anchor.Status != credentialstypes.CREDENTIAL_STATUS_ACTIVE {
		resp.Reason = "credential is revoked or inactive"
		return resp, nil
	}

	tmpl, ok := k.credentialsKeeper.GetTemplate(ctx, anchor.TemplateId)
	if !ok {
		resp.Reason = "credential template not found"
		return resp, nil
	}
	if len(tmpl.IssuerBbsPubkey) == 0 {
		resp.Reason = "template has no issuer BBS public key"
		return resp, nil
	}

	if k.verifier.VerifyBBSProof(req.Proof, tmpl.IssuerBbsPubkey, req.Nonce) {
		resp.Valid = true
	} else {
		resp.Reason = "proof verification failed"
	}
	return resp, nil
}
