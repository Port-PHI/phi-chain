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

type supplyReader interface {
	GetSupply(ctx context.Context, denom string) sdk.Coin
}

type penaltyEscrow interface {
	RedirectSlashedToPenalty(ctx sdk.Context, slashedUphi math.Int) error
}

type slashCompensator struct {
	slashingtypes.StakingKeeper // real staking keeper; non-slash methods pass through
	bank                        supplyReader
	penalty                     penaltyEscrow
}

var _ slashingtypes.StakingKeeper = slashCompensator{}

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
