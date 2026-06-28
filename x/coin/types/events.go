// SPDX-License-Identifier: Apache-2.0

package types

// coin module event keys.
const (
	EventTypeTransfer = "transfer"

	AttributeKeyFrom   = "from"
	AttributeKeyTo     = "to"
	AttributeKeyAmount = "amount"
	AttributeKeyBurned = "demurrage" // tiered coin-age demurrage burn (sent to the fee collector)
)
