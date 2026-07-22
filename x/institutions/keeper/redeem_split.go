// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"cosmossdk.io/math"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
)

// RedeemSplit decomposes surrendered uphi: UphiIn == Burned + ProtocolFee + Penalty + Dust.
type RedeemSplit struct {
	UphiIn      math.Int
	Burned      math.Int
	ProtocolFee math.Int
	Penalty     math.Int
	Dust        math.Int
	// Carved = ProtocolFee + Penalty + Dust, sent to phi_revenue.
	Carved math.Int
	// TomanOut is the rial paid: exactly the burned amount's toman value.
	TomanOut math.Int
}

// UphiPerToman returns k = UphiPerPhi / phi_to_toman, uphi per toman (canonically 10); exact since Params.Validate guarantees phi_to_toman divides UphiPerPhi.
func UphiPerToman(phiToToman uint64) math.Int {
	return math.NewIntFromUint64(cointypes.UphiPerPhi).Quo(math.NewIntFromUint64(phiToToman))
}

// ComputeRedeemSplit divides surrendered uphi into {burned, protocol fee, penalty, dust}.
func ComputeRedeemSplit(uphiIn, protocolFee, penalty math.Int, phiToToman uint64) RedeemSplit {
	k := UphiPerToman(phiToToman)

	// Clamp: carve can never exceed what was surrendered (unreachable given Validate's rate bounds; belt on a consensus-critical path).
	carve0 := protocolFee.Add(penalty)
	if carve0.GT(uphiIn) {
		carve0 = uphiIn
		if protocolFee.GT(uphiIn) {
			protocolFee = uphiIn
		}
		penalty = carve0.Sub(protocolFee)
	}

	// Round the burn DOWN to a multiple of k (uphiIn is itself a multiple of k).
	burnable := uphiIn.Sub(carve0)
	burned := burnable.Sub(burnable.Mod(k))

	// Rule 3: carve consumed all but a sub-toman remainder.
	if !burned.IsPositive() && uphiIn.GTE(k) {
		burned = k
		shortfall := k.Sub(burnable)
		fromPenalty := math.MinInt(shortfall, penalty)
		penalty = penalty.Sub(fromPenalty)
		protocolFee = protocolFee.Sub(math.MinInt(shortfall.Sub(fromPenalty), protocolFee))
	}

	carved := uphiIn.Sub(burned) // = protocolFee + penalty + dust, by construction
	return RedeemSplit{
		UphiIn:      uphiIn,
		Burned:      burned,
		ProtocolFee: protocolFee,
		Penalty:     penalty,
		Dust:        carved.Sub(protocolFee).Sub(penalty),
		Carved:      carved,
		TomanOut:    burned.Quo(k), // exact: burned is a multiple of k
	}
}
