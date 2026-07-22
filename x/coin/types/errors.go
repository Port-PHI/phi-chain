// SPDX-License-Identifier: Apache-2.0

package types

import "cosmossdk.io/errors"

// coin module errors (starting from code 2).
var (
	ErrInvalidAmount     = errors.Register(ModuleName, 2, "invalid amount")
	ErrInsufficientFunds = errors.Register(ModuleName, 3, "insufficient funds")
	ErrInvalidFeeTable   = errors.Register(ModuleName, 4, "invalid fee table")
	ErrInvalidParams     = errors.Register(ModuleName, 5, "invalid params")
	ErrSameAccount       = errors.Register(ModuleName, 6, "from and to accounts are identical")
	// ErrNoPayoutAddress guards the only exit from phi_revenue: with no governed destination set, a withdrawal is refused rather than sent anywhere by default.
	ErrNoPayoutAddress = errors.Register(ModuleName, 7, "company_payout_address is not set")
)
