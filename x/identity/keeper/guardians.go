// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"cosmossdk.io/errors"
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

// SetGuardianSet stores a DID's guardian set, replacing any previous set wholesale (never a merge).
func (k Keeper) SetGuardianSet(ctx sdk.Context, gs types.GuardianSet) {
	ctx.KVStore(k.storeKey).Set(types.GuardiansKey(gs.Did), k.cdc.MustMarshal(&gs))
}

// DeleteGuardianSet removes a DID's guardian set entirely (used when the DID becomes REVOKED).
func (k Keeper) DeleteGuardianSet(ctx sdk.Context, did string) {
	ctx.KVStore(k.storeKey).Delete(types.GuardiansKey(did))
}

// GetGuardians returns a DID's guardian set.
func (k Keeper) GetGuardians(ctx sdk.Context, did string) (types.GuardianSet, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.GuardiansKey(did))
	if bz == nil {
		return types.GuardianSet{}, false
	}
	var gs types.GuardianSet
	k.cdc.MustUnmarshal(bz, &gs)
	return gs, true
}

// IterateGuardians iterates over every guardian set; returning true stops the loop.
func (k Keeper) IterateGuardians(ctx sdk.Context, cb func(types.GuardianSet) bool) {
	store := ctx.KVStore(k.storeKey)
	it := storetypes.KVStorePrefixIterator(store, types.GuardiansPrefix)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var gs types.GuardianSet
		k.cdc.MustUnmarshal(it.Value(), &gs)
		if cb(gs) {
			break
		}
	}
}

func (k Keeper) validateGuardianSetCap(ctx sdk.Context, commitments [][]byte) error {
	maxGuardians := k.GetParams(ctx).MaxGuardians
	if uint64(len(commitments)) > uint64(maxGuardians) {
		return errors.Wrapf(types.ErrInvalidGuardians,
			"guardian count %d exceeds max_guardians %d", len(commitments), maxGuardians)
	}
	return nil
}

func (k Keeper) openGuardianCommitment(ctx sdk.Context, protectedDID, guardianDID string, salt []byte, signer string) error {
	// bound the opening before hashing; do not rely on ValidateBasic having run
	if len(salt) != types.GuardianSaltLen {
		return errors.Wrapf(types.ErrInvalidGuardians, "salt length %d (must be %d)", len(salt), types.GuardianSaltLen)
	}
	gs, found := k.GetGuardians(ctx, protectedDID)
	if !found {
		return errors.Wrapf(types.ErrInvalidGuardians, "did %s has no guardian set", protectedDID)
	}
	if !gs.HasCommitment(types.GuardianCommitment(guardianDID, salt)) {
		return errors.Wrapf(types.ErrNotAGuardian, "guardian %s", guardianDID)
	}
	if guardianDID == protectedDID {
		return errors.Wrap(types.ErrInvalidGuardians, "a DID cannot be its own guardian")
	}
	doc, found := k.GetIdentity(ctx, guardianDID)
	if !found {
		return errors.Wrapf(types.ErrGuardianNotEligible, "guardian %s does not exist", guardianDID)
	}
	if doc.Status != types.DID_STATUS_ACTIVE {
		return errors.Wrapf(types.ErrGuardianNotEligible, "guardian %s is not ACTIVE", guardianDID)
	}
	if doc.Controller != signer {
		return errors.Wrapf(types.ErrUnauthorized,
			"signer does not control guardian %s (a revealed opening is public and must not be replayable)", guardianDID)
	}
	return nil
}
