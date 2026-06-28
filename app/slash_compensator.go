// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
)

// supplyReader reads the uphi total supply (the subset of the bank keeper the compensator needs).
type supplyReader interface {
	GetSupply(ctx context.Context, denom string) sdk.Coin
}

// penaltyEscrow re-mints a measured uphi amount into circulation at the penalty destination.
type penaltyEscrow interface {
	RedirectSlashedToPenalty(ctx sdk.Context, slashedUphi math.Int) error
}

// slashCompensator wraps the staking keeper that x/slashing (and any future x/evidence) calls for
// slashing. uphi is the staking bond denom and is fully backed by institution vaults, so any burn of
// it must be compensated or the global solvency invariant (supply×phi_to_toman == Σvault×1e6) breaks.
//
// A validator slash at a past infraction height burns three things: the validator's bonded tokens
// AND the slashed portion of every unbonding-delegation and redelegation (x/staking keeper.Slash).
// The compensator does not reconstruct that total from the validator-direct effective fraction (which
// omits the unbonding/redelegation burns); it measures the ACTUAL uphi supply delta across the whole
// Slash call and re-mints exactly that amount to the penalty destination, so the net supply change of
// a slash is zero and solvency cannot be broken by routine slashing. The slashed value accrues to the
// operator/governance account instead of being destroyed.
type slashCompensator struct {
	slashingtypes.StakingKeeper // the real staking keeper; every non-slash method passes through
	bank                        supplyReader
	penalty                     penaltyEscrow
}

var _ slashingtypes.StakingKeeper = slashCompensator{}

// newSlashCompensator wraps sk. penalty is taken as a pointer because the institutions keeper that
// implements it is constructed after the slashing keeper in app wiring; the pointer late-binds to the
// final keeper before any block (and therefore any slash) is processed.
func newSlashCompensator(sk slashingtypes.StakingKeeper, bank supplyReader, penalty penaltyEscrow) slashCompensator {
	return slashCompensator{StakingKeeper: sk, bank: bank, penalty: penalty}
}

// Slash measures and compensates the whole-slash uphi supply delta.
func (c slashCompensator) Slash(ctx context.Context, consAddr sdk.ConsAddress, infractionHeight, power int64, slashFactor math.LegacyDec) (math.Int, error) {
	return c.compensate(ctx, func() (math.Int, error) {
		return c.StakingKeeper.Slash(ctx, consAddr, infractionHeight, power, slashFactor)
	})
}

// SlashWithInfractionReason measures and compensates the whole-slash uphi supply delta.
func (c slashCompensator) SlashWithInfractionReason(ctx context.Context, consAddr sdk.ConsAddress, infractionHeight, power int64, slashFactor math.LegacyDec, infraction stakingtypes.Infraction) (math.Int, error) {
	return c.compensate(ctx, func() (math.Int, error) {
		return c.StakingKeeper.SlashWithInfractionReason(ctx, consAddr, infractionHeight, power, slashFactor, infraction)
	})
}

// compensate records uphi supply before and after the wrapped slash and re-mints the burned delta to
// the penalty destination. The internal SlashWithInfractionReason→Slash delegation inside the staking
// keeper stays on the embedded keeper, so a single slash is measured (and compensated) exactly once.
func (c slashCompensator) compensate(ctx context.Context, slash func() (math.Int, error)) (math.Int, error) {
	before := c.bank.GetSupply(ctx, cointypes.Denom).Amount
	burned, err := slash()
	if err != nil {
		return burned, err
	}
	delta := before.Sub(c.bank.GetSupply(ctx, cointypes.Denom).Amount)
	if delta.IsPositive() {
		if err := c.penalty.RedirectSlashedToPenalty(sdk.UnwrapSDKContext(ctx), delta); err != nil {
			return burned, err
		}
	}
	return burned, nil
}
