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
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipient sdk.AccAddress, amt sdk.Coins) error
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	GetSupply(ctx context.Context, denom string) sdk.Coin
}

// IdentityKeeper is the interface required from x/identity: resolving an address to the human behind it.
type IdentityKeeper interface {
	// SubjectDID resolves a controller address to the DID that identifies it, REGARDLESS of that DID's status.
	SubjectDID(ctx sdk.Context, controller string) (string, bool)
}

// FeeCollectorName is the name of the fee-collector module account (auth).
const FeeCollectorName = "fee_collector"

// CoinsOf converts a uphi amount into sdk.Coins.
func CoinsOf(amount math.Int) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin(Denom, amount))
}
