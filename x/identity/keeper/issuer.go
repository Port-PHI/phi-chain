// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"context"

	"cosmossdk.io/errors"
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/Port-PHI/phi-chain/phicrypto"
	"github.com/Port-PHI/phi-chain/x/identity/types"
)

const (
	issuerAttestationDomain = "phi-issuer-attestation-v3"
	keyRotationDomain       = "phi-key-rotation-v3"
)

// SetTrustedIssuer stores (or replaces) a trusted issuer.
func (k Keeper) SetTrustedIssuer(ctx sdk.Context, ti types.TrustedIssuer) {
	ctx.KVStore(k.storeKey).Set(types.TrustedIssuerKey(ti.Did), k.cdc.MustMarshal(&ti))
}

// GetTrustedIssuer returns a trusted issuer by DID.
func (k Keeper) GetTrustedIssuer(ctx sdk.Context, did string) (types.TrustedIssuer, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.TrustedIssuerKey(did))
	if bz == nil {
		return types.TrustedIssuer{}, false
	}
	var ti types.TrustedIssuer
	k.cdc.MustUnmarshal(bz, &ti)
	return ti, true
}

// IsTrustedIssuer reports whether the DID is a registered, active issuer.
func (k Keeper) IsTrustedIssuer(ctx sdk.Context, did string) bool {
	ti, found := k.GetTrustedIssuer(ctx, did)
	return found && ti.Active
}

// IterateTrustedIssuers iterates over all registered issuers; returning true stops.
func (k Keeper) IterateTrustedIssuers(ctx sdk.Context, cb func(types.TrustedIssuer) bool) {
	store := ctx.KVStore(k.storeKey)
	it := storetypes.KVStorePrefixIterator(store, types.TrustedIssuerPrefix)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var ti types.TrustedIssuer
		k.cdc.MustUnmarshal(it.Value(), &ti)
		if cb(ti) {
			break
		}
	}
}

func (k Keeper) hasIssuerNonce(ctx sdk.Context, issuerDid string, nonce []byte) bool {
	return ctx.KVStore(k.storeKey).Has(types.IssuerNonceKey(issuerDid, nonce))
}

func (k Keeper) markIssuerNonce(ctx sdk.Context, issuerDid string, nonce []byte) {
	ctx.KVStore(k.storeKey).Set(types.IssuerNonceKey(issuerDid, nonce), []byte{1})
}

func attestationMessage(chainID, did string, pubKey, uniquenessHash []byte, creator string, nonce []byte) []byte {
	return types.CanonicalMessage(issuerAttestationDomain,
		[]byte(chainID), []byte(did), pubKey, uniquenessHash, []byte(creator), nonce)
}

func rotationMessage(chainID, did string, newPubKey []byte, creator string) []byte {
	return types.CanonicalMessage(keyRotationDomain,
		[]byte(chainID), []byte(did), newPubKey, []byte(creator))
}

func (k Keeper) verifyRegistration(ctx sdk.Context, msg *types.MsgRegisterIdentity) error {
	// Registrant's curve; unknown → reject, never default (the curve decides who can sign as this identity).
	curve, err := types.CurveForKeyType(msg.KeyType)
	if err != nil {
		return errors.Wrap(types.ErrInvalidPubKey, err.Error())
	}
	// DID must be the canonical derivation of pub_key on its own curve; fails closed on off-curve keys.
	derived, err := types.DeriveDIDForKeyType(msg.KeyType, msg.PubKey)
	if err != nil {
		return errors.Wrap(types.ErrInvalidPubKey, "public key is not a valid point on its curve")
	}
	if msg.Did != derived {
		return errors.Wrap(types.ErrInvalidDID, "did is not the canonical derivation of pub_key")
	}
	issuer, found := k.GetTrustedIssuer(ctx, msg.IssuerDid)
	if !found || !issuer.Active {
		return errors.Wrapf(types.ErrIssuerNotTrusted, "issuer %s", msg.IssuerDid)
	}
	// Single-use attestation nonce per issuer; marker written on success in RegisterIdentity.
	if k.hasIssuerNonce(ctx, msg.IssuerDid, msg.Nonce) {
		return errors.Wrapf(types.ErrNonceReused, "issuer %s nonce", msg.IssuerDid)
	}
	m := attestationMessage(ctx.ChainID(), msg.Did, msg.PubKey, msg.UniquenessHash, msg.Creator, msg.Nonce)
	// Issuer attestation: always P-256, never the registrant's curve (fail-closed).
	if !k.verifier.VerifySignature(phicrypto.Secp256r1, issuer.PubKey, m, msg.IssuerSig) {
		return errors.Wrap(types.ErrInvalidIssuerSig, "issuer attestation did not verify")
	}
	// Registrant proof-of-possession over the same message, on its own curve (fail-closed).
	if !k.verifier.VerifySignature(curve, msg.PubKey, m, msg.PopSig) {
		return errors.Wrap(types.ErrInvalidPoP, "proof-of-possession did not verify")
	}
	return nil
}

// RegisterTrustedIssuer adds or updates a trusted identity issuer — governance authority only.
func (k msgServer) RegisterTrustedIssuer(goCtx context.Context, msg *types.MsgRegisterTrustedIssuer) (*types.MsgRegisterTrustedIssuerResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if msg.Authority != k.authority {
		return nil, errors.Wrapf(govtypes.ErrInvalidSigner, "expected %s, got %s", k.authority, msg.Authority)
	}
	k.SetTrustedIssuer(ctx, msg.Issuer)
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeTrustedIssuerRegistered,
		sdk.NewAttribute(types.AttributeKeyIssuerDID, msg.Issuer.Did),
		sdk.NewAttribute(types.AttributeKeyActive, boolToStr(msg.Issuer.Active)),
	))
	return &types.MsgRegisterTrustedIssuerResponse{}, nil
}

// RevokeTrustedIssuer deactivates a trusted issuer (kept on the record) — governance authority only.
func (k msgServer) RevokeTrustedIssuer(goCtx context.Context, msg *types.MsgRevokeTrustedIssuer) (*types.MsgRevokeTrustedIssuerResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if msg.Authority != k.authority {
		return nil, errors.Wrapf(govtypes.ErrInvalidSigner, "expected %s, got %s", k.authority, msg.Authority)
	}
	ti, found := k.GetTrustedIssuer(ctx, msg.Did)
	if !found {
		return nil, errors.Wrapf(types.ErrIssuerNotFound, "did %s", msg.Did)
	}
	ti.Active = false
	k.SetTrustedIssuer(ctx, ti)
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeTrustedIssuerRevoked,
		sdk.NewAttribute(types.AttributeKeyIssuerDID, msg.Did),
	))
	return &types.MsgRevokeTrustedIssuerResponse{}, nil
}

func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
