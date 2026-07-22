// SPDX-License-Identifier: Apache-2.0

// Package keeper implements the x/credentials state machine.
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

// NewKeeper builds a new keeper; verifier is the phi-crypto port.
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

func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}

func (k Keeper) GetParams(ctx sdk.Context) (p types.Params) {
	bz := ctx.KVStore(k.storeKey).Get(types.ParamsKey)
	if bz == nil {
		return types.DefaultParams()
	}
	k.cdc.MustUnmarshal(bz, &p)
	return p
}

// SetParams validates and stores the parameters.
func (k Keeper) SetParams(ctx sdk.Context, p types.Params) error {
	if err := p.Validate(); err != nil {
		return err
	}
	ctx.KVStore(k.storeKey).Set(types.ParamsKey, k.cdc.MustMarshal(&p))
	return nil
}

func (k Keeper) SetTemplate(ctx sdk.Context, t types.CredentialTemplate) {
	ctx.KVStore(k.storeKey).Set(types.TemplateKey(t.Id), k.cdc.MustMarshal(&t))
}

func (k Keeper) GetTemplate(ctx sdk.Context, id string) (types.CredentialTemplate, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.TemplateKey(id))
	if bz == nil {
		return types.CredentialTemplate{}, false
	}
	var t types.CredentialTemplate
	k.cdc.MustUnmarshal(bz, &t)
	return t, true
}

func (k Keeper) HasTemplate(ctx sdk.Context, id string) bool {
	return ctx.KVStore(k.storeKey).Has(types.TemplateKey(id))
}

// IterateTemplates iterates all templates; return true to stop.
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

func (k Keeper) SetAnchor(ctx sdk.Context, a types.CredentialAnchor) {
	ctx.KVStore(k.storeKey).Set(types.AnchorKey(a.CredentialHash), k.cdc.MustMarshal(&a))
}

func (k Keeper) GetAnchor(ctx sdk.Context, hash []byte) (types.CredentialAnchor, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.AnchorKey(hash))
	if bz == nil {
		return types.CredentialAnchor{}, false
	}
	var a types.CredentialAnchor
	k.cdc.MustUnmarshal(bz, &a)
	return a, true
}

func (k Keeper) HasAnchor(ctx sdk.Context, hash []byte) bool {
	return ctx.KVStore(k.storeKey).Has(types.AnchorKey(hash))
}

// IterateAnchors iterates all anchors; return true to stop.
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

func (k Keeper) SetAgreement(ctx sdk.Context, a types.Agreement) {
	ctx.KVStore(k.storeKey).Set(types.AgreementKey(a.Hash), k.cdc.MustMarshal(&a))
}

func (k Keeper) GetAgreement(ctx sdk.Context, hash []byte) (types.Agreement, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.AgreementKey(hash))
	if bz == nil {
		return types.Agreement{}, false
	}
	var a types.Agreement
	k.cdc.MustUnmarshal(bz, &a)
	return a, true
}

func (k Keeper) HasAgreement(ctx sdk.Context, hash []byte) bool {
	return ctx.KVStore(k.storeKey).Has(types.AgreementKey(hash))
}

// IterateAgreements iterates all agreements; return true to stop.
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

func (k Keeper) SetPersonalAnchor(ctx sdk.Context, p types.PersonalAnchor) {
	ctx.KVStore(k.storeKey).Set(types.PersonalKey(p.OwnerDid, p.AnchorHash), k.cdc.MustMarshal(&p))
}

func (k Keeper) GetPersonalAnchor(ctx sdk.Context, ownerDID string, hash []byte) (types.PersonalAnchor, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.PersonalKey(ownerDID, hash))
	if bz == nil {
		return types.PersonalAnchor{}, false
	}
	var p types.PersonalAnchor
	k.cdc.MustUnmarshal(bz, &p)
	return p, true
}

func (k Keeper) HasPersonalAnchor(ctx sdk.Context, ownerDID string, hash []byte) bool {
	return ctx.KVStore(k.storeKey).Has(types.PersonalKey(ownerDID, hash))
}

// IteratePersonalAnchors iterates all personal anchors; return true to stop.
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

func (k Keeper) requireActiveDID(ctx sdk.Context, did string) error {
	doc, ok := k.identityKeeper.GetIdentity(ctx, did)
	if !ok || doc.Status != identitytypes.DID_STATUS_ACTIVE {
		return types.ErrDIDNotActive
	}
	return nil
}

func (k Keeper) verifyP256(pubKey, msg, sig []byte) bool {
	return k.verifier.VerifySignature(phicrypto.Secp256r1, pubKey, msg, sig)
}
