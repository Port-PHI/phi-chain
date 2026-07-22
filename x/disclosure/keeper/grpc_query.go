// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"bytes"
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	credentialstypes "github.com/Port-PHI/phi-chain/x/credentials/types"
	"github.com/Port-PHI/phi-chain/x/disclosure/types"
)

var _ types.QueryServer = Keeper{}

func (k Keeper) Params(goCtx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	return &types.QueryParamsResponse{Params: k.GetParams(ctx)}, nil
}

// VerifyDisclosure verifies a BBS+ selective-disclosure proof against an anchored, non-revoked credential; read-only, nothing persisted ("verify and forget").
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
	resp.DisclosurePolicyHash = tmpl.DisclosurePolicyHash

	// A pinned policy hash that no longer matches means the policy changed since audit — refuse.
	if len(req.DisclosurePolicyHash) != 0 && !bytes.Equal(req.DisclosurePolicyHash, tmpl.DisclosurePolicyHash) {
		resp.Reason = "disclosure policy hash mismatch: the template's policy has changed since it was pinned"
		return resp, nil
	}

	// Revealed set read from the proof envelope itself (never the caller); malformed fails closed.
	disclosed, err := types.ParseDisclosedProof(req.Proof)
	if err != nil {
		resp.Reason = "malformed proof envelope: " + err.Error()
		return resp, nil
	}
	if disclosed.MessageCount != tmpl.MessageCount {
		resp.Reason = "proof message_count does not match the template's declared claim count"
		return resp, nil
	}

	// Every revealed claim must be one the issuer declared disclosable.
	for _, i := range disclosed.RevealedIndices {
		if !tmpl.IsDisclosable(i) {
			resp.Reason = fmt.Sprintf("claim %d is not disclosable under this template's policy", i)
			return resp, nil
		}
	}

	// Only now the cryptography.
	if !k.verifier.VerifyBBSProof(req.Proof, tmpl.IssuerBbsPubkey, req.Nonce) {
		resp.Reason = "proof verification failed"
		return resp, nil
	}

	// L1 (revealed nothing) or L2 (revealed a permitted subset) — derived from the proof, never asserted by the caller.
	resp.Valid = true
	resp.Level = disclosed.Level()
	resp.RevealedIndices = disclosed.RevealedIndices
	return resp, nil
}
