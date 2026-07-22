// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"context"

	"cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

// This file enforces the "phi validator" rule as StakingHooks: every validator must be a unique active DID with at least the configured self-stake.

// StakingValidatorSource is the minimal staking-keeper access the hook needs (interface avoids an import cycle).
type StakingValidatorSource interface {
	GetValidator(ctx context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error)
}

// ValidatorHooks is the StakingHooks implementation for the phi validator rule.
type ValidatorHooks struct {
	k            Keeper
	sk           StakingValidatorSource
	minSelfStake math.Int
}

var _ stakingtypes.StakingHooks = ValidatorHooks{}

// NewValidatorHooks creates new hooks (minSelfStake in uphi).
func (k Keeper) NewValidatorHooks(sk StakingValidatorSource, minSelfStake math.Int) ValidatorHooks {
	return ValidatorHooks{k: k, sk: sk, minSelfStake: minSelfStake}
}

// AfterValidatorCreated is the validator gate: a unique active DID + minimum self-stake.
func (h ValidatorHooks) AfterValidatorCreated(ctx context.Context, valAddr sdk.ValAddress) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	// The rule applies only to post-genesis validator creation; the founding set is staked off-chain.
	if sdkCtx.BlockHeight() == 0 {
		return nil
	}

	// The account must have an active DID, resolved via the bounded (controller ‖ did) index, not a scan.
	acc := sdk.AccAddress(valAddr.Bytes()).String()
	did, ok := h.k.PrimaryDID(sdkCtx, acc)
	if !ok {
		return errors.Wrapf(types.ErrValidatorNeedsDID, "account %s", acc)
	}

	// That DID must not already be bound to another validator.
	if bound, exists := h.k.ValidatorForDID(sdkCtx, did); exists && bound != valAddr.String() {
		return errors.Wrapf(types.ErrDIDAlreadyValidator, "did %s already backs %s", did, bound)
	}

	// Minimum self-stake floor: min_self_delegation >= protocol floor.
	val, err := h.sk.GetValidator(ctx, valAddr)
	if err != nil {
		return err
	}
	if val.MinSelfDelegation.LT(h.minSelfStake) {
		return errors.Wrapf(types.ErrMinSelfDelegation, "min_self_delegation %s < floor %s uphi", val.MinSelfDelegation, h.minSelfStake)
	}

	h.k.BindValidatorToDID(sdkCtx, did, valAddr.String())
	return nil
}

// AfterValidatorRemoved releases the DID-to-validator binding.
func (h ValidatorHooks) AfterValidatorRemoved(ctx context.Context, _ sdk.ConsAddress, valAddr sdk.ValAddress) error {
	h.k.UnbindValidator(sdk.UnwrapSDKContext(ctx), valAddr.String())
	return nil
}

// The remaining hooks are no-ops.

func (h ValidatorHooks) BeforeValidatorModified(context.Context, sdk.ValAddress) error { return nil }
func (h ValidatorHooks) AfterValidatorBonded(context.Context, sdk.ConsAddress, sdk.ValAddress) error {
	return nil
}
func (h ValidatorHooks) AfterValidatorBeginUnbonding(context.Context, sdk.ConsAddress, sdk.ValAddress) error {
	return nil
}
func (h ValidatorHooks) BeforeDelegationCreated(context.Context, sdk.AccAddress, sdk.ValAddress) error {
	return nil
}
func (h ValidatorHooks) BeforeDelegationSharesModified(context.Context, sdk.AccAddress, sdk.ValAddress) error {
	return nil
}
func (h ValidatorHooks) BeforeDelegationRemoved(context.Context, sdk.AccAddress, sdk.ValAddress) error {
	return nil
}
func (h ValidatorHooks) AfterDelegationModified(context.Context, sdk.AccAddress, sdk.ValAddress) error {
	return nil
}

// BeforeValidatorSlashed is intentionally a no-op: supply-constant slash compensation is done by the slash-compensation wrapper around the staking keeper (see app), not here.
func (h ValidatorHooks) BeforeValidatorSlashed(context.Context, sdk.ValAddress, math.LegacyDec) error {
	return nil
}
func (h ValidatorHooks) AfterUnbondingInitiated(context.Context, uint64) error { return nil }
