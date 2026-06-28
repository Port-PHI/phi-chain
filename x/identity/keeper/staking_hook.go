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

// This file enforces the "phi validator" rule as StakingHooks: every validator must be a unique
// verified human (an active DID) with at least the configured self-stake. An error in the
// validator-creation hook rejects the MsgCreateValidator transaction.

// StakingValidatorSource is the minimal staking-keeper access the hook needs (an interface to avoid an import cycle).
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

// NewValidatorHooks creates new hooks (minSelfStake in uphi). Keeping total uphi supply constant
// across a slash is handled outside these hooks, by the slash-compensation wrapper on the staking
// keeper the slashing module calls (see app: it measures the whole-slash supply delta and re-mints
// it to the penalty destination). A hook cannot do this correctly: BeforeValidatorSlashed fires
// after the SDK has already burned unbonding-delegation/redelegation balances, so it can only see
// the validator-direct burn.
func (k Keeper) NewValidatorHooks(sk StakingValidatorSource, minSelfStake math.Int) ValidatorHooks {
	return ValidatorHooks{k: k, sk: sk, minSelfStake: minSelfStake}
}

// AfterValidatorCreated is the validator gate: a unique active DID + minimum self-stake.
func (h ValidatorHooks) AfterValidatorCreated(ctx context.Context, valAddr sdk.ValAddress) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	// At genesis (InitChain, height 0) the founding validator set is staked off-chain;
	// the rule applies only to runtime validator creation (post-genesis), guarding against later Sybil entry.
	if sdkCtx.BlockHeight() == 0 {
		return nil
	}

	// The validator's corresponding account must have an active DID.
	acc := sdk.AccAddress(valAddr.Bytes()).String()
	did, ok := h.k.FindActiveDIDByController(sdkCtx, acc)
	if !ok {
		return errors.Wrapf(types.ErrValidatorNeedsDID, "account %s", acc)
	}

	// That DID must not already be bound to another validator.
	if bound, exists := h.k.ValidatorForDID(sdkCtx, did.Did); exists && bound != valAddr.String() {
		return errors.Wrapf(types.ErrDIDAlreadyValidator, "did %s already backs %s", did.Did, bound)
	}

	// Minimum self-stake: the validator's min_self_delegation >= protocol floor. Since staking
	// guarantees self-delegation >= min_self_delegation (or jail), this enforces the stake floor.
	val, err := h.sk.GetValidator(ctx, valAddr)
	if err != nil {
		return err
	}
	if val.MinSelfDelegation.LT(h.minSelfStake) {
		return errors.Wrapf(types.ErrMinSelfDelegation, "min_self_delegation %s < floor %s uphi", val.MinSelfDelegation, h.minSelfStake)
	}

	h.k.BindValidatorToDID(sdkCtx, did.Did, valAddr.String())
	return nil
}

// AfterValidatorRemoved releases the DID-to-validator binding.
func (h ValidatorHooks) AfterValidatorRemoved(ctx context.Context, _ sdk.ConsAddress, valAddr sdk.ValAddress) error {
	h.k.UnbindValidator(sdk.UnwrapSDKContext(ctx), valAddr.String())
	return nil
}

// --- The remaining hooks are no-ops ---

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

// BeforeValidatorSlashed is intentionally a no-op. Keeping total uphi supply constant under slashing
// is NOT done here: this hook fires in the middle of x/staking keeper.Slash, after the SDK has
// already burned the slashed unbonding-delegation and redelegation balances, so `fraction ×
// validator.Tokens` is only the validator-direct (bonded) burn — compensating it would leave the
// unbonding/redelegation burns uncompensated and silently break the solvency invariant
// (supply×phi_to_toman == Σvault×1e6). The whole-slash supply delta is instead measured and re-minted
// by the slash-compensation wrapper around the staking keeper that x/slashing calls (see app).
func (h ValidatorHooks) BeforeValidatorSlashed(context.Context, sdk.ValAddress, math.LegacyDec) error {
	return nil
}
func (h ValidatorHooks) AfterUnbondingInitiated(context.Context, uint64) error { return nil }
