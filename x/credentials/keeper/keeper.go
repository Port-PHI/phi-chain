// SPDX-License-Identifier: Apache-2.0

// Package keeper implements the x/credentials state machine: credential
// templates, credential anchors, multi-party agreements and personal anchors.
// Signature checks are delegated to phi-crypto through the phicrypto.Verifier
// port (never hand-rolled crypto).
package keeper

import (
	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/phicrypto"
	"github.com/Port-PHI/phi-chain/x/credentials/types"
	identitytypes "github.com/Port-PHI/phi-chain/x/identity/types"
)

// Keeper manages the x/credentials state.
type Keeper struct {
	cdc            codec.BinaryCodec
	storeKey       storetypes.StoreKey
	authority      string
	identityKeeper types.IdentityKeeper
	verifier       phicrypto.Verifier
}

// NewKeeper builds a new keeper. verifier is the phi-crypto port; in production
// app wiring it is phicrypto.Default() (Disabled unless built with the cgo tag),
// and tests inject phicrypto.Fake.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeKey storetypes.StoreKey,
	authority string,
	identity types.IdentityKeeper,
	verifier phicrypto.Verifier,
) Keeper {
	return Keeper{
		cdc:            cdc,
		storeKey:       storeKey,
		authority:      authority,
		identityKeeper: identity,
		verifier:       verifier,
	}
}

// GetAuthority returns the governance authority address.
func (k Keeper) GetAuthority() string { return k.authority }

// Logger returns the module logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}

// --- params ---

// GetParams returns the current parameters.
func (k Keeper) GetParams(ctx sdk.Context) (p types.Params) {
	bz := ctx.KVStore(k.storeKey).Get(types.ParamsKey)
	if bz == nil {
		return types.DefaultParams()
	}
	k.cdc.MustUnmarshal(bz, &p)
	return p
}

// SetParams stores the parameters after validation.
func (k Keeper) SetParams(ctx sdk.Context, p types.Params) error {
	if err := p.Validate(); err != nil {
		return err
	}
	ctx.KVStore(k.storeKey).Set(types.ParamsKey, k.cdc.MustMarshal(&p))
	return nil
}

// --- credential templates ---

// SetTemplate stores a template.
func (k Keeper) SetTemplate(ctx sdk.Context, t types.CredentialTemplate) {
	ctx.KVStore(k.storeKey).Set(types.TemplateKey(t.Id), k.cdc.MustMarshal(&t))
}

// GetTemplate reads a template by id.
func (k Keeper) GetTemplate(ctx sdk.Context, id string) (types.CredentialTemplate, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.TemplateKey(id))
	if bz == nil {
		return types.CredentialTemplate{}, false
	}
	var t types.CredentialTemplate
	k.cdc.MustUnmarshal(bz, &t)
	return t, true
}

// HasTemplate reports whether a template id exists.
func (k Keeper) HasTemplate(ctx sdk.Context, id string) bool {
	return ctx.KVStore(k.storeKey).Has(types.TemplateKey(id))
}

// IterateTemplates iterates all templates; returning true stops the loop.
func (k Keeper) IterateTemplates(ctx sdk.Context, cb func(types.CredentialTemplate) bool) {
	it := storetypes.KVStorePrefixIterator(ctx.KVStore(k.storeKey), types.TemplatePrefix)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var t types.CredentialTemplate
		k.cdc.MustUnmarshal(it.Value(), &t)
		if cb(t) {
			break
		}
	}
}

// --- credential anchors ---

// SetAnchor stores a credential anchor.
func (k Keeper) SetAnchor(ctx sdk.Context, a types.CredentialAnchor) {
	ctx.KVStore(k.storeKey).Set(types.AnchorKey(a.CredentialHash), k.cdc.MustMarshal(&a))
}

// GetAnchor reads a credential anchor by hash.
func (k Keeper) GetAnchor(ctx sdk.Context, hash []byte) (types.CredentialAnchor, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.AnchorKey(hash))
	if bz == nil {
		return types.CredentialAnchor{}, false
	}
	var a types.CredentialAnchor
	k.cdc.MustUnmarshal(bz, &a)
	return a, true
}

// HasAnchor reports whether a credential hash is anchored.
func (k Keeper) HasAnchor(ctx sdk.Context, hash []byte) bool {
	return ctx.KVStore(k.storeKey).Has(types.AnchorKey(hash))
}

// IterateAnchors iterates all anchors; returning true stops the loop.
func (k Keeper) IterateAnchors(ctx sdk.Context, cb func(types.CredentialAnchor) bool) {
	it := storetypes.KVStorePrefixIterator(ctx.KVStore(k.storeKey), types.AnchorPrefix)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var a types.CredentialAnchor
		k.cdc.MustUnmarshal(it.Value(), &a)
		if cb(a) {
			break
		}
	}
}

// --- agreements ---

// SetAgreement stores an agreement.
func (k Keeper) SetAgreement(ctx sdk.Context, a types.Agreement) {
	ctx.KVStore(k.storeKey).Set(types.AgreementKey(a.Hash), k.cdc.MustMarshal(&a))
}

// GetAgreement reads an agreement by hash.
func (k Keeper) GetAgreement(ctx sdk.Context, hash []byte) (types.Agreement, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.AgreementKey(hash))
	if bz == nil {
		return types.Agreement{}, false
	}
	var a types.Agreement
	k.cdc.MustUnmarshal(bz, &a)
	return a, true
}

// HasAgreement reports whether an agreement hash exists.
func (k Keeper) HasAgreement(ctx sdk.Context, hash []byte) bool {
	return ctx.KVStore(k.storeKey).Has(types.AgreementKey(hash))
}

// IterateAgreements iterates all agreements; returning true stops the loop.
func (k Keeper) IterateAgreements(ctx sdk.Context, cb func(types.Agreement) bool) {
	it := storetypes.KVStorePrefixIterator(ctx.KVStore(k.storeKey), types.AgreementPrefix)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var a types.Agreement
		k.cdc.MustUnmarshal(it.Value(), &a)
		if cb(a) {
			break
		}
	}
}

// --- personal anchors ---

// SetPersonalAnchor stores a personal anchor.
func (k Keeper) SetPersonalAnchor(ctx sdk.Context, p types.PersonalAnchor) {
	ctx.KVStore(k.storeKey).Set(types.PersonalKey(p.OwnerDid, p.AnchorHash), k.cdc.MustMarshal(&p))
}

// GetPersonalAnchor reads a personal anchor by owner DID and hash.
func (k Keeper) GetPersonalAnchor(ctx sdk.Context, ownerDID string, hash []byte) (types.PersonalAnchor, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.PersonalKey(ownerDID, hash))
	if bz == nil {
		return types.PersonalAnchor{}, false
	}
	var p types.PersonalAnchor
	k.cdc.MustUnmarshal(bz, &p)
	return p, true
}

// HasPersonalAnchor reports whether a personal anchor exists.
func (k Keeper) HasPersonalAnchor(ctx sdk.Context, ownerDID string, hash []byte) bool {
	return ctx.KVStore(k.storeKey).Has(types.PersonalKey(ownerDID, hash))
}

// IteratePersonalAnchors iterates all personal anchors; returning true stops the loop.
func (k Keeper) IteratePersonalAnchors(ctx sdk.Context, cb func(types.PersonalAnchor) bool) {
	it := storetypes.KVStorePrefixIterator(ctx.KVStore(k.storeKey), types.PersonalPrefix)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var p types.PersonalAnchor
		k.cdc.MustUnmarshal(it.Value(), &p)
		if cb(p) {
			break
		}
	}
}

// --- identity / crypto helpers ---

// authDID resolves did via x/identity and asserts it is active and controlled by
// signer (a bech32 address). It returns the DID's public key for signature checks.
func (k Keeper) authDID(ctx sdk.Context, did, signer string) ([]byte, error) {
	doc, ok := k.identityKeeper.GetIdentity(ctx, did)
	if !ok || doc.Status != identitytypes.DID_STATUS_ACTIVE {
		return nil, types.ErrDIDNotActive
	}
	if doc.Controller != signer {
		return nil, types.ErrUnauthorized
	}
	return doc.PubKey, nil
}

// requireActiveDID asserts that did exists and is active (no controller check).
func (k Keeper) requireActiveDID(ctx sdk.Context, did string) error {
	doc, ok := k.identityKeeper.GetIdentity(ctx, did)
	if !ok || doc.Status != identitytypes.DID_STATUS_ACTIVE {
		return types.ErrDIDNotActive
	}
	return nil
}

// verifyP256 checks an ECDSA P-256 signature over msg via the phi-crypto port.
// With the default Disabled verifier this returns false (fail-safe) until the
// cgo build links libphi_crypto.
func (k Keeper) verifyP256(pubKey, msg, sig []byte) bool {
	return k.verifier.VerifySignature(phicrypto.Secp256r1, pubKey, msg, sig)
}
