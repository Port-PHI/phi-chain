// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"context"
	"fmt"
	"time"

	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

// The validator↔DID binding is a CONTINUOUS rule enforced by a sweep over the active set (bounded at MaxValidators), not an admission check and not hook-based: a sweep reads current truth and cannot be bypassed by an unhooked mutation path.

// ValidatorSweepStaking is the narrow staking access the sweep needs (iterate active set, jail); interface to avoid an import cycle.
type ValidatorSweepStaking interface {
	IterateLastValidators(ctx context.Context, fn func(index int64, validator stakingtypes.ValidatorI) (stop bool)) error
	Jail(ctx context.Context, consAddr sdk.ConsAddress) error
}

// ValidatorGenesisStaking is the read-only staking access the genesis founder cross-check needs.
type ValidatorGenesisStaking interface {
	GetAllValidators(ctx context.Context) ([]stakingtypes.Validator, error)
}

// ValidatorSweepSlashing is the slashing access to make a removal PERMANENT; Tombstone moves no stake and burns nothing (losing an identity is an eligibility failure, not a consensus fault).
type ValidatorSweepSlashing interface {
	Tombstone(ctx context.Context, consAddr sdk.ConsAddress) error
	IsTombstoned(ctx context.Context, consAddr sdk.ConsAddress) bool
	JailUntil(ctx context.Context, consAddr sdk.ConsAddress, jailTime time.Time) error
}

var permanentJailUntil = time.Unix(1<<62, 0).UTC()

// ValidatorSweepOutcome is what the sweep decided for one validator.
type ValidatorSweepOutcome uint8

const (
	// SweepKept: holds an ACTIVE DID and continues validating.
	SweepKept ValidatorSweepOutcome = iota
	// SweepBound: had no binding but its account holds an ACTIVE DID, so it was bound (carries the genesis founder set, created before the identity registry exists).
	SweepBound
	// SweepJailed: removed REVERSIBLY (SUSPENDED DID, no identity yet, or DID already backing another).
	SweepJailed
	// SweepTombstoned: removed PERMANENTLY; only a REVOKED identity (terminal, uniqueness marker spent).
	SweepTombstoned
)

// SweepValidatorBindings enforces the validator↔DID binding across the active set, returning the per-operator decision.
func (k Keeper) SweepValidatorBindings(ctx sdk.Context, sk ValidatorSweepStaking, slk ValidatorSweepSlashing) (map[string]ValidatorSweepOutcome, error) {
	type candidate struct {
		valoper  string
		consAddr sdk.ConsAddress
		jailed   bool
	}

	var actives []candidate
	if err := sk.IterateLastValidators(ctx, func(_ int64, v stakingtypes.ValidatorI) bool {
		consBytes, err := v.GetConsAddr()
		if err != nil {
			// Skip one unreadable validator (retried next block) rather than stop the block.
			k.Logger(ctx).Error("validator binding sweep: cannot read consensus address; skipping",
				"validator", v.GetOperator(), "error", err)
			return false
		}
		actives = append(actives, candidate{
			valoper:  v.GetOperator(),
			consAddr: sdk.ConsAddress(consBytes),
			jailed:   v.IsJailed(),
		})
		return false
	}); err != nil {
		// Iteration failed: skip this block rather than abort it; the sweep re-runs next block.
		k.Logger(ctx).Error("validator binding sweep: cannot iterate the validator set; skipping this block",
			"error", err)
		return map[string]ValidatorSweepOutcome{}, nil
	}

	// Phase 2: judge and act, in collection order.
	outcomes := make(map[string]ValidatorSweepOutcome, len(actives))
	for _, c := range actives {
		if outcome, ok := k.sweepOneIsolated(ctx, sk, slk, c.valoper, c.consAddr, c.jailed); ok {
			outcomes[c.valoper] = outcome
		}
	}
	return outcomes, nil
}

func (k Keeper) sweepOneIsolated(ctx sdk.Context, sk ValidatorSweepStaking, slk ValidatorSweepSlashing,
	valoper string, consAddr sdk.ConsAddress, alreadyJailed bool,
) (outcome ValidatorSweepOutcome, ok bool) {
	cacheCtx, writeCache := ctx.CacheContext()

	defer func() {
		if r := recover(); r != nil {
			k.Logger(ctx).Error("validator binding sweep: panic while judging a validator; skipping",
				"validator", valoper, "panic", fmt.Sprint(r))
			emitSweepFailed(ctx, valoper, "panic: "+fmt.Sprint(r))
			outcome, ok = SweepKept, false
		}
	}()

	outcome, err := k.sweepOne(cacheCtx, sk, slk, valoper, consAddr, alreadyJailed)
	if err != nil {
		k.Logger(ctx).Error("validator binding sweep: cannot act on a validator; skipping",
			"validator", valoper, "error", err)
		emitSweepFailed(ctx, valoper, err.Error())
		return SweepKept, false
	}

	writeCache()
	return outcome, true
}

func (k Keeper) sweepOne(ctx sdk.Context, sk ValidatorSweepStaking, slk ValidatorSweepSlashing,
	valoper string, consAddr sdk.ConsAddress, alreadyJailed bool,
) (ValidatorSweepOutcome, error) {
	did, bound := k.DIDForValidator(ctx, valoper)
	acc := sdk.AccAddress(sdk.ValAddress(mustValAddress(valoper)).Bytes()).String()

	if !bound {
		// No binding.
		primary, _, _, _ := k.ControllerSweepStatus(ctx, acc)
		if primary == "" {
			// No ACTIVE DID to bind to.
			return k.removeByIdentityStatus(ctx, sk, slk, valoper, acc, consAddr, alreadyJailed)
		}
		// One human, one validator: the same uniqueness rule AfterValidatorCreated applies.
		if other, exists := k.ValidatorForDID(ctx, primary); exists && other != valoper {
			return k.removeReversibly(ctx, sk, consAddr, valoper, primary, alreadyJailed)
		}
		k.BindValidatorToDID(ctx, primary, valoper)
		emitSweep(ctx, valoper, primary, "bound")
		return SweepBound, nil
	}

	doc, found := k.GetIdentity(ctx, did)
	if !found {
		// The binding names a DID that does not exist.
		k.UnbindValidator(ctx, valoper)
		return k.removeByIdentityStatus(ctx, sk, slk, valoper, acc, consAddr, alreadyJailed)
	}

	switch doc.Status {
	case types.DID_STATUS_ACTIVE:
		return SweepKept, nil

	case types.DID_STATUS_SUSPENDED:
		// Reversible: jail only, never tombstone, and the binding SURVIVES.
		if !alreadyJailed {
			if err := sk.Jail(ctx, consAddr); err != nil {
				return SweepJailed, err
			}
			emitSweep(ctx, valoper, did, "jailed")
		}
		return SweepJailed, nil

	default:
		// REVOKED, or any status that is not one of the two above: terminal.
		k.UnbindValidator(ctx, valoper)
		return k.removePermanently(ctx, sk, slk, consAddr, alreadyJailed)
	}
}

func (k Keeper) removeByIdentityStatus(ctx sdk.Context, sk ValidatorSweepStaking, slk ValidatorSweepSlashing,
	valoper, acc string, consAddr sdk.ConsAddress, alreadyJailed bool,
) (ValidatorSweepOutcome, error) {
	// The operator's identity status is read from the O(1) sweep record, not resolved by a bounded scan that ordering could truncate.
	_, hasSuspended, hasRevoked, _ := k.ControllerSweepStatus(ctx, acc)
	if hasRevoked && !hasSuspended {
		return k.removePermanently(ctx, sk, slk, consAddr, alreadyJailed)
	}
	// No ACTIVE DID to name in the event; the operator is jailed on the strength of its account, not a DID.
	return k.removeReversibly(ctx, sk, consAddr, valoper, "", alreadyJailed)
}

func (k Keeper) removeReversibly(ctx sdk.Context, sk ValidatorSweepStaking,
	consAddr sdk.ConsAddress, valoper, did string, alreadyJailed bool,
) (ValidatorSweepOutcome, error) {
	if alreadyJailed {
		return SweepJailed, nil
	}
	if err := sk.Jail(ctx, consAddr); err != nil {
		return SweepJailed, err
	}
	emitSweep(ctx, valoper, did, "jailed")
	return SweepJailed, nil
}

func (k Keeper) removePermanently(ctx sdk.Context, sk ValidatorSweepStaking, slk ValidatorSweepSlashing,
	consAddr sdk.ConsAddress, alreadyJailed bool,
) (ValidatorSweepOutcome, error) {
	if !alreadyJailed {
		if err := sk.Jail(ctx, consAddr); err != nil {
			return SweepTombstoned, err
		}
	}
	if !slk.IsTombstoned(ctx, consAddr) {
		// Errors are deliberately not propagated: see the doc comment.
		_ = slk.Tombstone(ctx, consAddr)
		_ = slk.JailUntil(ctx, consAddr, permanentJailUntil)
	}
	return SweepTombstoned, nil
}

func emitSweep(ctx sdk.Context, valoper, did, action string) {
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeValidatorBindingSweep,
		sdk.NewAttribute(types.AttributeKeyValidator, valoper),
		sdk.NewAttribute(types.AttributeKeyDID, did),
		sdk.NewAttribute(types.AttributeKeyAction, action),
	))
}

func emitSweepFailed(ctx sdk.Context, valoper, reason string) {
	telemetry.IncrCounter(1, types.ModuleName, "validator_sweep_failed")
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeValidatorSweepFailed,
		sdk.NewAttribute(types.AttributeKeyValidator, valoper),
		sdk.NewAttribute(types.AttributeKeyReason, reason),
	))
}

func mustValAddress(valoper string) sdk.ValAddress {
	addr, err := sdk.ValAddressFromBech32(valoper)
	if err != nil {
		return nil
	}
	return addr
}
