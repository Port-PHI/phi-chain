// SPDX-License-Identifier: Apache-2.0

package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BankKeeper is the x/bank surface for the social-recovery deposit.
type BankKeeper interface {
	// Escrow: initiator → identity module account.
	SendCoinsFromAccountToModule(ctx context.Context, sender sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	// Refund: identity module account → initiator (on execute).
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipient sdk.AccAddress, amt sdk.Coins) error
	// Forfeit: identity module account → fee collector (on cancel/expire/supersede).
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
}

// FeeCollectorName is the auth fee-collector account, destination of a forfeited recovery deposit.
const FeeCollectorName = "fee_collector"
