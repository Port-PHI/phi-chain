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
	// SendCoinsFromModuleToModule carves the fee/penalty out to phi_revenue; it never changes supply.
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
	GetSupply(ctx context.Context, denom string) sdk.Coin
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	// BlockedAddr reports whether an address is blocked from receiving funds.
	BlockedAddr(addr sdk.AccAddress) bool
}

// IdentityKeeper is the interface required from x/identity (bootstrap lock + controller→DID resolution for the redeem cap).
type IdentityKeeper interface {
	BootstrapPhase(ctx sdk.Context) bool
	// PrimaryDID resolves a controller to its ACTIVE DID; false → redeem cap fails closed, keying by address.
	PrimaryDID(ctx sdk.Context, controller string) (string, bool)
	// SubjectDID resolves a controller to its DID regardless of status; keys the per-human redeem bucket.
	SubjectDID(ctx sdk.Context, controller string) (string, bool)
}

// GovKeeper is the optional x/gov interface: fx finalization outside bootstrap must reference a PASSED public proposal.
type GovKeeper interface {
	Proposal(ctx context.Context, proposalID uint64) (govv1.Proposal, error)
}

// CoinKeeper is the interface required from x/coin (coin age: mint tracking + tiered redeem penalty).
type CoinKeeper interface {
	// AddCoins credits minted coin as a FIFO coin-age lot acquired at the given time.
	AddCoins(ctx sdk.Context, address string, amount math.Int, acquiredAt int64)
	// EarlyRedeemPenalty computes the tiered coin-age exit penalty (uphi) and decrements the seller's age buckets; moves no coin.
	EarlyRedeemPenalty(ctx sdk.Context, address string, redeemUphi math.Int) math.Int
}
