// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
)

type vaultTotalReader interface {
	SumVaultBalance(ctx sdk.Context) math.Int
}

type govGuardBank interface {
	govtypes.BankKeeper
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
}

type govBurnGuard struct {
	govGuardBank
	vaults vaultTotalReader
}

var _ govtypes.BankKeeper = govBurnGuard{}

func newGovBurnGuard(bank govGuardBank, vaults vaultTotalReader) govBurnGuard {
	return govBurnGuard{govGuardBank: bank, vaults: vaults}
}

// BurnCoins redirects a gov-module uphi burn to the fee collector while any vault is non-zero; every other burn passes through.
func (g govBurnGuard) BurnCoins(ctx context.Context, name string, amt sdk.Coins) error {
	if name == govtypes.ModuleName {
		uphi := amt.AmountOf(cointypes.Denom)
		if uphi.IsPositive() && g.vaults.SumVaultBalance(sdk.UnwrapSDKContext(ctx)).IsPositive() {
			redirect := cointypes.CoinsOf(uphi)
			if err := g.govGuardBank.SendCoinsFromModuleToModule(ctx, govtypes.ModuleName, authtypes.FeeCollectorName, redirect); err != nil {
				return err
			}
			// Burn any non-uphi remainder as usual: only vault-backed uphi is protected.
			if rest := amt.Sub(redirect...); !rest.IsZero() {
				return g.govGuardBank.BurnCoins(ctx, name, rest)
			}
			return nil
		}
	}
	return g.govGuardBank.BurnCoins(ctx, name, amt)
}
