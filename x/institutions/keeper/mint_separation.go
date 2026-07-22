// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

// Attest/mint separation: an institution must hold at least two distinct admin keys, and the key authorising a mint must not be the key that attested the reserve it mints against.

// SetLastAttestor records the address that published an institution's current reserve attestation.
func (k Keeper) SetLastAttestor(ctx sdk.Context, instID string, attestor sdk.AccAddress) {
	ctx.KVStore(k.storeKey).Set(types.LastAttestorKey(instID), attestor.Bytes())
}

// LastAttestor returns the address that published the current reserve attestation, and whether one exists.
func (k Keeper) LastAttestor(ctx sdk.Context, instID string) (sdk.AccAddress, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.LastAttestorKey(instID))
	if len(bz) == 0 {
		return nil, false
	}
	return sdk.AccAddress(bz), true
}

func (k Keeper) requireMintSeparation(ctx sdk.Context, inst types.Institution, minterBech string) error {
	if admins := k.countAdmins(ctx, inst); admins < types.MinAdminsForMint {
		return errors.Wrapf(types.ErrTooFewAdmins,
			"institution=%s has %d admin key(s), minting requires %d", inst.Id, admins, types.MinAdminsForMint)
	}

	minter, err := sdk.AccAddressFromBech32(minterBech)
	if err != nil {
		return err
	}
	attestor, found := k.LastAttestor(ctx, inst.Id)
	if !found {
		return errors.Wrapf(types.ErrAttestorIsMinter,
			"institution=%s has no recorded reserve attestor; publish an attestation before minting", inst.Id)
	}
	if attestor.Equals(minter) {
		return errors.Wrapf(types.ErrAttestorIsMinter,
			"institution=%s: %s attested the reserve and cannot also authorise the mint", inst.Id, minterBech)
	}
	return nil
}
