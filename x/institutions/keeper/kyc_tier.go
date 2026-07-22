// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"encoding/binary"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

// HolderKycTier returns the tier assigned to a holder at an institution, and whether one was assigned.
func (k Keeper) HolderKycTier(ctx sdk.Context, instID string, holder sdk.AccAddress) (uint32, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.HolderKycTierKey(instID, holder))
	if len(bz) != 4 {
		return 0, false
	}
	return binary.BigEndian.Uint32(bz), true
}

// SetHolderKycTier assigns a holder's KYC tier at an institution.
func (k Keeper) SetHolderKycTier(ctx sdk.Context, instID string, holder sdk.AccAddress, tier uint32) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], tier)
	ctx.KVStore(k.storeKey).Set(types.HolderKycTierKey(instID, holder), b[:])
}

func strictestKycLimit(p types.InstitutionParams) (math.Int, bool) {
	strictest := math.ZeroInt()
	found := false
	for _, kt := range p.KycTierLimits {
		lim := types.CapInt(kt.DailyLimitToman)
		if !lim.IsPositive() {
			continue
		}
		if !found || lim.LT(strictest) {
			strictest, found = lim, true
		}
	}
	return strictest, found
}

func (k Keeper) effectiveMintKycDailyLimit(ctx sdk.Context, inst types.Institution, recipient sdk.AccAddress, asserted uint32) math.Int {
	limit := kycTierDailyLimit(inst.Params, asserted)
	if !limit.IsPositive() {
		// The asserted tier is not configured: fail closed rather than open.
		strictest, found := strictestKycLimit(inst.Params)
		if !found {
			return math.ZeroInt() // the institution configured no KYC policy at all
		}
		limit = strictest
	}
	if recorded, ok := k.HolderKycTier(ctx, inst.Id, recipient); ok {
		if rl := kycTierDailyLimit(inst.Params, recorded); rl.IsPositive() && rl.LT(limit) {
			return rl
		}
	}
	return limit
}

func (k Keeper) effectiveKycDailyLimit(ctx sdk.Context, inst types.Institution, holder sdk.AccAddress) math.Int {
	if tier, ok := k.HolderKycTier(ctx, inst.Id, holder); ok {
		if lim := kycTierDailyLimit(inst.Params, tier); lim.IsPositive() {
			return lim
		}
	}
	strictest, found := strictestKycLimit(inst.Params)
	if !found {
		return math.ZeroInt() // the institution configured no KYC policy at all
	}
	return strictest
}
