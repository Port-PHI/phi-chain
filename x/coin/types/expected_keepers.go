// SPDX-License-Identifier: Apache-2.0

package types

import (
	"context"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BankKeeper is the x/bank interface required by the coin module.
type BankKeeper interface {
	SendCoins(ctx context.Context, from, to sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModule(ctx context.Context, sender sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	GetSupply(ctx context.Context, denom string) sdk.Coin
}

// FeeCollectorName is the name of the fee-collector module account (auth).
// The tiered coin-age demurrage burn goes to this account so the
// solvency invariant stays intact.
const FeeCollectorName = "fee_collector"

// CoinsOf converts a uphi amount into sdk.Coins.
func CoinsOf(amount math.Int) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin(Denom, amount))
}
