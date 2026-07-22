// SPDX-License-Identifier: Apache-2.0

package types

// coin module event keys.
const (
	EventTypeTransfer = "transfer"
	// EventTypeRevenueCollected is emitted once per (message type, stream) leg of a split fee.
	EventTypeRevenueCollected = "revenue_collected"
	// EventTypeRevenueWithdrawn is emitted on a governance-authorised withdrawal from phi_revenue.
	EventTypeRevenueWithdrawn = "revenue_withdrawn"

	AttributeKeyFrom    = "from"
	AttributeKeyTo      = "to"
	AttributeKeyAmount  = "amount"
	AttributeKeyStream  = "stream"
	AttributeKeyMsgType = "msg_type"
)

// Revenue streams.
const (
	StreamValidator = "validator"
	StreamCompany   = "company"
	// StreamProtocol is the protocol fee carved out of an institution mint/redeem.
	StreamProtocol = "protocol"
	// StreamPenalty is the early-redeem (coin-age) penalty carved out of a redemption, plus the rounding dust of that carve-out.
	StreamPenalty = "penalty"
)
