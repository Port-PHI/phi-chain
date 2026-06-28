// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"bytes"
	"context"

	"cosmossdk.io/errors"
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/Port-PHI/phi-chain/phicrypto"
	"github.com/Port-PHI/phi-chain/x/identity/types"
)

// TrustedIssuer registry (gov-managed) + the on-chain RegisterIdentity verification: a trusted issuer
// must attest the registration (issuer_sig) and the registrant must prove possession of pub_key
// (pop_sig), both over the canonical attestation message. Verification is fail-closed (the default
// build's verifier rejects), consistent with the rest of the chain's crypto posture.

const (
	// issuerAttestationDomain domain-separates the registration attestation message.
	issuerAttestationDomain = "phi-issuer-attestation-v1"
	// keyRotationDomain domain-separates the key-rotation proof-of-possession message.
	keyRotationDomain = "phi-key-rotation-v1"
)

// --- TrustedIssuer registry ---

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

// IterateTrustedIssuers iterates over all registered issuers (genesis export); returning true stops.
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

// --- Issuer attestation nonce (single-use anti-replay) ---

// hasIssuerNonce reports whether (issuerDid, nonce) was already consumed.
func (k Keeper) hasIssuerNonce(ctx sdk.Context, issuerDid string, nonce []byte) bool {
	return ctx.KVStore(k.storeKey).Has(types.IssuerNonceKey(issuerDid, nonce))
}

// markIssuerNonce records (issuerDid, nonce) as consumed so it cannot be reused.
func (k Keeper) markIssuerNonce(ctx sdk.Context, issuerDid string, nonce []byte) {
	ctx.KVStore(k.storeKey).Set(types.IssuerNonceKey(issuerDid, nonce), []byte{1})
}

// --- Canonical messages ---

// attestationMessage is the canonical message both issuer_sig and pop_sig cover:
//
//	domain ‖ 0x00 ‖ did ‖ 0x00 ‖ pub_key ‖ 0x00 ‖ uniqueness_hash ‖ 0x00 ‖ creator ‖ 0x00 ‖ nonce
func attestationMessage(did string, pubKey, uniquenessHash []byte, creator string, nonce []byte) []byte {
	return bytes.Join([][]byte{
		[]byte(issuerAttestationDomain),
		[]byte(did), pubKey, uniquenessHash, []byte(creator), nonce,
	}, []byte{0x00})
}

// rotationMessage is the canonical message pop_sig covers for a key rotation:
//
//	domain ‖ 0x00 ‖ did ‖ 0x00 ‖ new_pub_key ‖ 0x00 ‖ creator
func rotationMessage(did string, newPubKey []byte, creator string) []byte {
	return bytes.Join([][]byte{
		[]byte(keyRotationDomain), []byte(did), newPubKey, []byte(creator),
	}, []byte{0x00})
}

// verifyRegistration enforces the three Sybil-resistance checks: self-certifying DID, a trusted
// active issuer's attestation, and the registrant's proof-of-possession. Fail-closed.
func (k Keeper) verifyRegistration(ctx sdk.Context, msg *types.MsgRegisterIdentity) error {
	// 1. The DID must be the canonical derivation of pub_key (self-certifying; no arbitrary binding).
	if msg.Did != types.DeriveDIDFromP256(msg.PubKey) {
		return errors.Wrap(types.ErrInvalidDID, "did is not the canonical derivation of pub_key")
	}
	// 2. The issuer must be a registered, active trusted issuer.
	issuer, found := k.GetTrustedIssuer(ctx, msg.IssuerDid)
	if !found || !issuer.Active {
		return errors.Wrapf(types.ErrIssuerNotTrusted, "issuer %s", msg.IssuerDid)
	}
	// 2b. The attestation nonce must be single-use per issuer (the nonce is now persisted, so it
	// is genuine anti-replay rather than an inert field). The marker is written on success in
	// RegisterIdentity.
	if k.hasIssuerNonce(ctx, msg.IssuerDid, msg.Nonce) {
		return errors.Wrapf(types.ErrNonceReused, "issuer %s nonce", msg.IssuerDid)
	}
	m := attestationMessage(msg.Did, msg.PubKey, msg.UniquenessHash, msg.Creator, msg.Nonce)
	// 3. The issuer's attestation signature must verify (P-256, fail-closed).
	if !k.verifier.VerifySignature(phicrypto.Secp256r1, issuer.PubKey, m, msg.IssuerSig) {
		return errors.Wrap(types.ErrInvalidIssuerSig, "issuer attestation did not verify")
	}
	// 4. The registrant must prove possession of pub_key over the same message (fail-closed).
	if !k.verifier.VerifySignature(phicrypto.Secp256r1, msg.PubKey, m, msg.PopSig) {
		return errors.Wrap(types.ErrInvalidPoP, "proof-of-possession did not verify")
	}
	return nil
}

// --- Gov-only handlers ---

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

// boolToStr renders a bool for event attributes.
func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
