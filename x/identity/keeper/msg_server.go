// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/Port-PHI/phi-chain/phicrypto"
	"github.com/Port-PHI/phi-chain/x/identity/types"
)

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns a MsgServer implementation.
func NewMsgServerImpl(k Keeper) types.MsgServer {
	return &msgServer{Keeper: k}
}

var _ types.MsgServer = msgServer{}

// RegisterIdentity registers a phi identity.
//
// On-chain guarantees enforced here (one-human-one-DID Sybil resistance): (1) the DID is unique and
// self-certifies its pub_key (did == canonical derivation of pub_key); (2) a uniqueness marker is used
// at most once; (3) a trusted, active issuer attested (did, pub_key, uniqueness_hash, creator, nonce)
// — issuer_sig verifies; and (4) the registrant proves possession of pub_key — pop_sig verifies. The
// signature checks are fail-closed (the default build's verifier rejects), so production runs the
// `phicrypto_cgo` build. See docs/identity-issuer-verification.md.
func (k msgServer) RegisterIdentity(goCtx context.Context, msg *types.MsgRegisterIdentity) (*types.MsgRegisterIdentityResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if k.HasIdentity(ctx, msg.Did) {
		return nil, errors.Wrapf(types.ErrIdentityExists, "did %s", msg.Did)
	}
	if k.HasUniqueness(ctx, msg.UniquenessHash) {
		return nil, errors.Wrap(types.ErrUniquenessUsed, "this human already holds a phi identity")
	}
	// Issuer attestation + proof-of-possession (fail-closed).
	if err := k.verifyRegistration(ctx, msg); err != nil {
		return nil, err
	}

	doc := types.DIDDocument{
		Did:            msg.Did,
		Controller:     msg.Creator,
		PubKey:         msg.PubKey,
		UniquenessHash: msg.UniquenessHash,
		CreatedAt:      ctx.BlockTime().Unix(),
		Status:         types.DID_STATUS_ACTIVE,
	}
	k.SetIdentity(ctx, doc)
	k.setUniqueness(ctx, msg.UniquenessHash, msg.Did)
	// Consume the issuer attestation nonce so it cannot be replayed.
	k.markIssuerNonce(ctx, msg.IssuerDid, msg.Nonce)

	// Counter + one-way bootstrap latch.
	count := k.GetIdentityCount(ctx) + 1
	k.SetIdentityCount(ctx, count)
	params := k.GetParams(ctx)
	if params.BootstrapPhase && count >= params.BootstrapThreshold {
		params.BootstrapPhase = false // irreversible: the counter only increases
		if err := k.SetParams(ctx, params); err != nil {
			return nil, err // propagate rather than swallow
		}
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeBootstrapEnded,
			sdk.NewAttribute(types.AttributeKeyCount, fmt.Sprintf("%d", count)),
		))
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRegisterIdentity,
		sdk.NewAttribute(types.AttributeKeyDID, msg.Did),
		sdk.NewAttribute(types.AttributeKeyController, msg.Creator),
		sdk.NewAttribute(types.AttributeKeyIssuerDID, msg.IssuerDid),
	))
	return &types.MsgRegisterIdentityResponse{}, nil
}

// RevokeIdentity revokes a DID: controller or governance only.
func (k msgServer) RevokeIdentity(goCtx context.Context, msg *types.MsgRevokeIdentity) (*types.MsgRevokeIdentityResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	doc, found := k.GetIdentity(ctx, msg.Did)
	if !found {
		return nil, errors.Wrapf(types.ErrIdentityNotFound, "did %s", msg.Did)
	}
	if msg.Creator != doc.Controller && msg.Creator != k.authority {
		return nil, errors.Wrap(types.ErrUnauthorized, "only controller or governance may revoke")
	}
	if doc.Status == types.DID_STATUS_REVOKED {
		return nil, types.ErrIdentityRevoked
	}

	doc.Status = types.DID_STATUS_REVOKED
	k.SetIdentity(ctx, doc)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRevokeIdentity,
		sdk.NewAttribute(types.AttributeKeyDID, msg.Did),
	))
	return &types.MsgRevokeIdentityResponse{}, nil
}

// RotateIdentityKey rotates a DID's passkey: the current controller authorizes (tx signer) and the
// new key's possession is proven (pop_sig over the canonical rotation message). The DID identifier,
// controller, and uniqueness marker are preserved; only pub_key changes. Fail-closed.
func (k msgServer) RotateIdentityKey(goCtx context.Context, msg *types.MsgRotateIdentityKey) (*types.MsgRotateIdentityKeyResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	doc, found := k.GetIdentity(ctx, msg.Did)
	if !found {
		return nil, errors.Wrapf(types.ErrIdentityNotFound, "did %s", msg.Did)
	}
	if msg.Creator != doc.Controller {
		return nil, errors.Wrap(types.ErrUnauthorized, "only the current controller may rotate the key")
	}
	if doc.Status != types.DID_STATUS_ACTIVE {
		return nil, errors.Wrap(types.ErrKeyRotation, "cannot rotate the key of a non-active identity")
	}
	// Proof-of-possession of the new key (fail-closed).
	m := rotationMessage(msg.Did, msg.NewPubKey, msg.Creator)
	if !k.verifier.VerifySignature(phicrypto.Secp256r1, msg.NewPubKey, m, msg.PopSig) {
		return nil, errors.Wrap(types.ErrInvalidPoP, "new-key proof-of-possession did not verify")
	}

	doc.PubKey = msg.NewPubKey // DID identifier, controller, and uniqueness marker are unchanged
	k.SetIdentity(ctx, doc)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRotateIdentityKey,
		sdk.NewAttribute(types.AttributeKeyDID, msg.Did),
		sdk.NewAttribute(types.AttributeKeyController, doc.Controller),
	))
	return &types.MsgRotateIdentityKeyResponse{}, nil
}

// UpdateParams updates the module parameters: governance authority only.
func (k msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if msg.Authority != k.authority {
		return nil, errors.Wrapf(govtypes.ErrInvalidSigner, "expected %s, got %s", k.authority, msg.Authority)
	}
	if err := k.SetParams(ctx, msg.Params); err != nil {
		return nil, err
	}
	return &types.MsgUpdateParamsResponse{}, nil
}
