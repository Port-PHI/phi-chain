// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

// This file maintains the "one unique DID per validator" binding: a two-way did/valoper mapping.

// FindActiveDIDByController returns the first active DID owned by a controller account.
func (k Keeper) FindActiveDIDByController(ctx sdk.Context, controller string) (types.DIDDocument, bool) {
	var found types.DIDDocument
	ok := false
	k.IterateIdentities(ctx, func(d types.DIDDocument) bool {
		if d.Controller == controller && d.Status == types.DID_STATUS_ACTIVE {
			found, ok = d, true
			return true
		}
		return false
	})
	return found, ok
}

// BindValidatorToDID records the two-way DID-to-validator binding.
func (k Keeper) BindValidatorToDID(ctx sdk.Context, did, valoper string) {
	store := ctx.KVStore(k.storeKey)
	store.Set(types.DIDToValidatorKey(did), []byte(valoper))
	store.Set(types.ValidatorToDIDKey(valoper), []byte(did))
}

// ValidatorForDID returns the validator bound to a DID, if any.
func (k Keeper) ValidatorForDID(ctx sdk.Context, did string) (string, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.DIDToValidatorKey(did))
	if bz == nil {
		return "", false
	}
	return string(bz), true
}

// DIDForValidator returns the DID bound to a validator.
func (k Keeper) DIDForValidator(ctx sdk.Context, valoper string) (string, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.ValidatorToDIDKey(valoper))
	if bz == nil {
		return "", false
	}
	return string(bz), true
}

// UnbindValidator clears the DID-to-validator binding (on validator removal; the DID is released).
func (k Keeper) UnbindValidator(ctx sdk.Context, valoper string) {
	store := ctx.KVStore(k.storeKey)
	if did, ok := k.DIDForValidator(ctx, valoper); ok {
		store.Delete(types.DIDToValidatorKey(did))
	}
	store.Delete(types.ValidatorToDIDKey(valoper))
}
