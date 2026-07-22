// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"encoding/hex"

	"cosmossdk.io/errors"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	"github.com/Port-PHI/phi-chain/x/identity/types"
)

func hexID(id []byte) string { return hex.EncodeToString(id) }

// SetRecoveryRequest stores a request and keeps the per-DID index in step.
func (k Keeper) SetRecoveryRequest(ctx sdk.Context, r types.RecoveryRequest) {
	store := ctx.KVStore(k.storeKey)
	store.Set(types.RecoveryKey(r.RecoveryId), k.cdc.MustMarshal(&r))
	store.Set(types.RecoveryByDIDKey(r.Did, r.RecoveryId), []byte{1})
}

func (k Keeper) deleteRecoveryRequest(ctx sdk.Context, r types.RecoveryRequest) {
	store := ctx.KVStore(k.storeKey)
	store.Delete(types.RecoveryKey(r.RecoveryId))
	store.Delete(types.RecoveryByDIDKey(r.Did, r.RecoveryId))
	k.deleteRecoveryTallyEpoch(ctx, r.RecoveryId)
}

// GetRecoveryRequest returns a request by id.
func (k Keeper) GetRecoveryRequest(ctx sdk.Context, recoveryID []byte) (types.RecoveryRequest, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.RecoveryKey(recoveryID))
	if bz == nil {
		return types.RecoveryRequest{}, false
	}
	var r types.RecoveryRequest
	k.cdc.MustUnmarshal(bz, &r)
	return r, true
}

// IterateRecoveryRequests iterates over every request; returning true stops the loop.
func (k Keeper) IterateRecoveryRequests(ctx sdk.Context, cb func(types.RecoveryRequest) bool) {
	store := ctx.KVStore(k.storeKey)
	it := storetypes.KVStorePrefixIterator(store, types.RecoveryPrefix)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var r types.RecoveryRequest
		k.cdc.MustUnmarshal(it.Value(), &r)
		if cb(r) {
			break
		}
	}
}

// RecoveryRequestsForDID returns every request recorded for a DID via the per-DID index (bounded scan).
func (k Keeper) RecoveryRequestsForDID(ctx sdk.Context, did string) []types.RecoveryRequest {
	store := ctx.KVStore(k.storeKey)
	prefix := types.RecoveryByDIDPrefixFor(did)
	it := storetypes.KVStorePrefixIterator(store, prefix)
	defer it.Close()
	out := []types.RecoveryRequest{}
	for ; it.Valid(); it.Next() {
		id := it.Key()[len(prefix):]
		if r, found := k.GetRecoveryRequest(ctx, id); found {
			out = append(out, r)
		}
	}
	return out
}

func (k Keeper) hasRecoveryNonce(ctx sdk.Context, did string, nonce []byte) bool {
	return ctx.KVStore(k.storeKey).Has(types.RecoveryNonceKey(did, nonce))
}

func (k Keeper) markRecoveryNonce(ctx sdk.Context, did string, nonce []byte) {
	ctx.KVStore(k.storeKey).Set(types.RecoveryNonceKey(did, nonce), []byte{1})
}

func (k Keeper) reapIfExpired(ctx sdk.Context, r *types.RecoveryRequest) (terminal bool, err error) {
	if types.IsTerminalRecoveryStatus(r.Status) {
		return true, nil
	}
	// held: live when the suspension began, so its window is paused; both halves required
	if r.FrozenAt != 0 && k.recoveryFrozenBySuspension(ctx, r.Did) {
		return false, nil
	}
	if ctx.BlockTime().Unix() <= r.ExpiresAt {
		return false, nil
	}
	if err := k.forfeitDeposit(ctx, *r); err != nil {
		return true, err
	}
	r.Status = types.RECOVERY_STATUS_EXPIRED
	k.deleteRecoveryRequest(ctx, *r)
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRecoveryExpired,
		sdk.NewAttribute(types.AttributeKeyRecoveryID, hexID(r.RecoveryId)),
		sdk.NewAttribute(types.AttributeKeyDID, r.Did),
	))
	return true, nil
}

func (k Keeper) recoveryFrozenBySuspension(ctx sdk.Context, did string) bool {
	doc, found := k.GetIdentity(ctx, did)
	return found && doc.Status == types.DID_STATUS_SUSPENDED
}

func (k Keeper) freezeLiveRecoveries(ctx sdk.Context, did string) {
	now := ctx.BlockTime().Unix()
	for _, r := range k.RecoveryRequestsForDID(ctx, did) {
		if types.IsTerminalRecoveryStatus(r.Status) || r.FrozenAt != 0 {
			continue
		}
		if now > r.ExpiresAt {
			continue // already expired when the freeze arrived; it stays expired and forfeits on touch
		}
		r.FrozenAt = now
		k.SetRecoveryRequest(ctx, r)
	}
}

func (k Keeper) thawRecoveries(ctx sdk.Context, did string) {
	now := ctx.BlockTime().Unix()
	ttl := int64(k.GetParams(ctx).RecoveryRequestTtlSeconds)
	for _, r := range k.RecoveryRequestsForDID(ctx, did) {
		if r.FrozenAt == 0 || types.IsTerminalRecoveryStatus(r.Status) {
			continue
		}
		// Preserve the remaining veto time before the freeze stamp is cleared: the request regains exactly the head-start it had left when the freeze began, measured forward from now.
		if r.FrozenAt < r.ExecuteAfter {
			r.ExecuteAfter = now + (r.ExecuteAfter - r.FrozenAt)
		}
		r.FrozenAt = 0
		r.ExpiresAt = now + ttl
		k.SetRecoveryRequest(ctx, r)
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeRecoveryExtended,
			sdk.NewAttribute(types.AttributeKeyRecoveryID, hexID(r.RecoveryId)),
			sdk.NewAttribute(types.AttributeKeyDID, r.Did),
		))
	}
}

func (k Keeper) countOpenRequests(ctx sdk.Context, did string) (uint32, error) {
	var open uint32
	for _, r := range k.RecoveryRequestsForDID(ctx, did) {
		req := r
		terminal, err := k.reapIfExpired(ctx, &req)
		if err != nil {
			return 0, err
		}
		if !terminal {
			open++
		}
	}
	return open, nil
}

// EVERY movement below is supply-neutral.

func (k Keeper) escrowDeposit(ctx sdk.Context, from sdk.AccAddress, amount math.Int) error {
	if !amount.IsPositive() {
		return nil
	}
	return k.bankKeeper.SendCoinsFromAccountToModule(ctx, from, types.ModuleName, cointypes.CoinsOf(amount))
}

func (k Keeper) refundDeposit(ctx sdk.Context, r types.RecoveryRequest) error {
	amount, ok := math.NewIntFromString(r.DepositUphi)
	if !ok || !amount.IsPositive() {
		return nil
	}
	to, err := sdk.AccAddressFromBech32(r.ProposedNewController)
	if err != nil {
		return err
	}
	return k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, to, cointypes.CoinsOf(amount))
}

func (k Keeper) forfeitDeposit(ctx sdk.Context, r types.RecoveryRequest) error {
	amount, ok := math.NewIntFromString(r.DepositUphi)
	if !ok || !amount.IsPositive() {
		return nil
	}
	return k.bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, types.FeeCollectorName, cointypes.CoinsOf(amount))
}

func (k Keeper) supersedeSiblings(ctx sdk.Context, did string, executed []byte) error {
	for _, r := range k.RecoveryRequestsForDID(ctx, did) {
		if string(r.RecoveryId) == string(executed) || types.IsTerminalRecoveryStatus(r.Status) {
			continue
		}
		if err := k.forfeitDeposit(ctx, r); err != nil {
			return err
		}
		r.Status = types.RECOVERY_STATUS_SUPERSEDED
		k.deleteRecoveryRequest(ctx, r)
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeRecoverySuperseded,
			sdk.NewAttribute(types.AttributeKeyRecoveryID, hexID(r.RecoveryId)),
			sdk.NewAttribute(types.AttributeKeyDID, did),
		))
	}
	return nil
}

func (k Keeper) terminateRecoveryRequestsForDID(ctx sdk.Context, did string) error {
	for _, r := range k.RecoveryRequestsForDID(ctx, did) {
		if types.IsTerminalRecoveryStatus(r.Status) {
			continue
		}
		if err := k.forfeitDeposit(ctx, r); err != nil {
			return err
		}
		r.Status = types.RECOVERY_STATUS_SUPERSEDED
		k.deleteRecoveryRequest(ctx, r)
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeRecoverySuperseded,
			sdk.NewAttribute(types.AttributeKeyRecoveryID, hexID(r.RecoveryId)),
			sdk.NewAttribute(types.AttributeKeyDID, did),
		))
	}
	return nil
}

func (k Keeper) assertNoKeyCollision(ctx sdk.Context, keyType types.KeyType, pubKey []byte) error {
	var derived string
	var err error
	switch keyType {
	case types.KEY_TYPE_SECP256R1:
		derived, err = types.DeriveDIDFromP256(pubKey)
	default:
		return errors.Wrapf(types.ErrInvalidRecovery, "unsupported key_type %s", keyType)
	}
	if err != nil {
		return errors.Wrap(types.ErrInvalidPubKey, "proposed_new_pub_key is not a valid curve point")
	}
	if k.HasIdentity(ctx, derived) {
		return errors.Wrapf(types.ErrRecoveryKeyCollision, "key derives the registered did %s", derived)
	}
	return nil
}
