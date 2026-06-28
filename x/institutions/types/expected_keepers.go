// SPDX-License-Identifier: Apache-2.0

package types

import (
	"context"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
)

// BankKeeper is the interface the institutions module requires from x/bank.
type BankKeeper interface {
	MintCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
	BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipient sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModule(ctx context.Context, sender sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	GetSupply(ctx context.Context, denom string) sdk.Coin
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	// BlockedAddr reports whether an address is blocked from receiving funds: the penalty
	// destination must not be a blocked address, or routing slashed-stake compensation to it would fail.
	BlockedAddr(addr sdk.AccAddress) bool
}

// IdentityKeeper is the interface required from x/identity (bootstrap lock for institution onboarding).
type IdentityKeeper interface {
	BootstrapPhase(ctx sdk.Context) bool
}

// GovKeeper is the (optional) interface required from x/gov: fx onboarding finalization outside the
// bootstrap phase must reference a PASSED public proposal. Injected via Keeper.SetGovKeeper after the
// gov keeper is constructed (it is built after the institutions keeper in app wiring).
type GovKeeper interface {
	Proposal(ctx context.Context, proposalID uint64) (govv1.Proposal, error)
}

// CoinKeeper is the interface required from x/coin (coin age: mint tracking + tiered redeem penalty).
type CoinKeeper interface {
	// AddYoungCoins adds newly minted coin to the recipient's young bucket.
	AddYoungCoins(ctx sdk.Context, address string, amount math.Int, youngSince int64)
	// RedeemDemurrage computes the tiered exit penalty (uphi) based on the seller's coin age and decrements the buckets.
	RedeemDemurrage(ctx sdk.Context, address string, redeemUphi math.Int) math.Int
}
