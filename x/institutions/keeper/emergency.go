// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"context"

	"cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

func emergencyCapForElapsed(em types.EmergencyRedemption, elapsed int64) (math.Int, bool) {
	switch {
	case elapsed < types.EmergencyDay30:
		return types.CapInt(em.CapBeforeDay30), true
	case elapsed < types.EmergencyDay60:
		return emergencyCapOrDefault(em.CapFromDay30, types.DefaultEmergencyCapFromDay30Toman), true
	case elapsed < types.EmergencyDay90:
		return emergencyCapOrDefault(em.CapFromDay60, types.DefaultEmergencyCapFromDay60Toman), true
	default:
		return math.ZeroInt(), false
	}
}

func emergencyCapOrDefault(v, def string) math.Int {
	if v == "" {
		return types.CapInt(def)
	}
	return types.CapInt(v)
}

func defaultIfEmpty(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// SetEmergencyRedemption activates/deactivates the stepped-redemption brake — governance authority only.
func (k msgServer) SetEmergencyRedemption(goCtx context.Context, msg *types.MsgSetEmergencyRedemption) (*types.MsgSetEmergencyRedemptionResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if msg.Authority != k.authority {
		return nil, errors.Wrapf(govtypes.ErrInvalidSigner, "expected %s, got %s", k.authority, msg.Authority)
	}

	params := k.GetParams(ctx)
	em := params.EmergencyRedemption
	if msg.Active {
		now := ctx.BlockTime().Unix()
		// Stamp started_at only on a genuinely fresh emergency (first activation, or re-activation after full elapse >= day 90); prevents restarting the halt by toggling Active.
		if em.StartedAt == 0 || (!em.Active && now-em.StartedAt >= types.EmergencyDay90) {
			em.StartedAt = now
		}
		em.Active = true
		em.CapBeforeDay30 = msg.CapBeforeDay30
		em.CapFromDay30 = defaultIfEmpty(msg.CapFromDay30, types.DefaultEmergencyCapFromDay30Toman)
		em.CapFromDay60 = defaultIfEmpty(msg.CapFromDay60, types.DefaultEmergencyCapFromDay60Toman)
	} else {
		em.Active = false
	}
	params.EmergencyRedemption = em
	if err := k.SetParams(ctx, params); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeEmergencyRedemption,
		sdk.NewAttribute(types.AttributeKeyActive, boolToStr(em.Active)),
		sdk.NewAttribute(types.AttributeKeyStartedAt, math.NewInt(em.StartedAt).String()),
	))
	return &types.MsgSetEmergencyRedemptionResponse{}, nil
}
