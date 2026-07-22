// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

// One-unique-DID-per-validator binding: a two-way did/valoper mapping.

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

// SubjectDID returns the DID a controller is keyed BY for per-human state, regardless of status, via the bounded (controller ‖ did) prefix scan; false only when the controller holds no DID.
func (k Keeper) SubjectDID(ctx sdk.Context, controller string) (string, bool) {
	did, found, _ := k.subjectDIDBounded(ctx, controller)
	return did, found
}

func (k Keeper) subjectDIDBounded(ctx sdk.Context, controller string) (did string, found, truncated bool) {
	store := ctx.KVStore(k.storeKey)
	prefix := types.ControllerIndexPrefixFor(controller)
	it := storetypes.KVStorePrefixIterator(store, prefix)
	defer it.Close()
	scanned := 0
	for ; it.Valid(); it.Next() {
		if scanned == types.MaxControllerDIDScan {
			return "", false, true
		}
		scanned++
		candidate := string(it.Key()[len(prefix):])
		if _, ok := k.GetIdentity(ctx, candidate); ok {
			return candidate, true, false
		}
	}
	return "", false, false
}

func (k Keeper) refreshControllerSweepStatus(ctx sdk.Context, controller string) {
	store := ctx.KVStore(k.storeKey)

	firstActive := ""
	hasSuspended, hasRevoked, hasAny := false, false, false
	prefix := types.ControllerIndexPrefixFor(controller)
	it := storetypes.KVStorePrefixIterator(store, prefix)
	for ; it.Valid(); it.Next() {
		did := string(it.Key()[len(prefix):])
		d, found := k.GetIdentity(ctx, did)
		if !found {
			continue
		}
		hasAny = true
		switch d.Status {
		case types.DID_STATUS_ACTIVE:
			if firstActive == "" {
				firstActive = d.Did
			}
		case types.DID_STATUS_SUSPENDED:
			hasSuspended = true
		case types.DID_STATUS_REVOKED:
			hasRevoked = true
		}
	}
	_ = it.Close()

	key := types.ControllerSweepKey(controller)
	if !hasAny {
		store.Delete(key)
		return
	}
	store.Set(key, types.EncodeControllerSweep(firstActive, hasSuspended, hasRevoked))
}

// ControllerSweepStatus reports in O(1) the operator's first ACTIVE DID (empty when none) and whether it holds any SUSPENDED or REVOKED DID; hasRecord is false when it controls no DID at all.
func (k Keeper) ControllerSweepStatus(ctx sdk.Context, controller string) (activeDID string, hasSuspended, hasRevoked, hasRecord bool) {
	return types.DecodeControllerSweep(ctx.KVStore(k.storeKey).Get(types.ControllerSweepKey(controller)))
}

// PrimaryDID returns the controller's ACTIVE DID via a bounded (controller ‖ did) prefix scan (safe on the consensus hot path); first in deterministic key order if several.
func (k Keeper) PrimaryDID(ctx sdk.Context, controller string) (string, bool) {
	did, found, _ := k.primaryDIDBounded(ctx, controller)
	return did, found
}

func (k Keeper) primaryDIDBounded(ctx sdk.Context, controller string) (did string, found, truncated bool) {
	store := ctx.KVStore(k.storeKey)
	prefix := types.ControllerIndexPrefixFor(controller)
	it := storetypes.KVStorePrefixIterator(store, prefix)
	defer it.Close()
	scanned := 0
	for ; it.Valid(); it.Next() {
		if scanned == types.MaxControllerDIDScan {
			return "", false, true
		}
		scanned++
		candidate := string(it.Key()[len(prefix):])
		if d, ok := k.GetIdentity(ctx, candidate); ok && d.Status == types.DID_STATUS_ACTIVE {
			return d.Did, true, false
		}
	}
	return "", false, false
}
