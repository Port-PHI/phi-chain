// SPDX-License-Identifier: Apache-2.0

package types

import "cosmossdk.io/math"

// RevenueLeg is one (message type, stream) contribution to a collected fee.
type RevenueLeg struct {
	MsgTypeURL string
	Stream     string
	Amount     math.Int
}

// FeeSplit is the routing decision for one transaction's fixed fee: Total is deducted from the payer, Validator goes to the standard fee collector and Company to the keyless phi_revenue account.
type FeeSplit struct {
	Total     math.Int
	Validator math.Int
	Company   math.Int
	Legs      []RevenueLeg
}

// NewFeeSplit returns a zeroed split (math.Int's zero value is unusable, so it must be built).
func NewFeeSplit() FeeSplit {
	return FeeSplit{Total: math.ZeroInt(), Validator: math.ZeroInt(), Company: math.ZeroInt()}
}
