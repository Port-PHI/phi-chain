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

// Emergency stepped-redemption brake. When governance activates it, each holder's cumulative
// redemption per institution is capped, and the cap relaxes over time: before day 30 → halted; from
// day 30 → 200 PHI; from day 60 → 2,000 PHI; from day 90 → unlimited. Enforcement lives in
// enforceRedeemCaps/addRedeemCounters (rbac.go); the cumulative counter is keyed by the activation
// timestamp so each new emergency starts a fresh bucket.

// emergencyCapForElapsed returns the per-holder cumulative redeem cap (Toman) for the elapsed seconds
// since activation, and whether a cap applies (false = unlimited, i.e. day 90+).
func emergencyCapForElapsed(em types.EmergencyRedemption, elapsed int64) (math.Int, bool) {
	switch {
	case elapsed < types.EmergencyDay30:
		// Pre-relief window: defaults to "0" = halted (governance may set a positive floor).
		return types.CapInt(em.CapBeforeDay30), true
	case elapsed < types.EmergencyDay60:
		return emergencyCapOrDefault(em.CapFromDay30, types.DefaultEmergencyCapFromDay30Toman), true
	case elapsed < types.EmergencyDay90:
		return emergencyCapOrDefault(em.CapFromDay60, types.DefaultEmergencyCapFromDay60Toman), true
	default:
		return math.ZeroInt(), false // day 90+: full settlement (unlimited)
	}
}

// emergencyCapOrDefault uses the default cap only when the field is empty (an explicit "0" stays a halt).
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
		// Stamp started_at only on a GENUINELY fresh emergency: the first activation, or a
		// re-activation after the previous window has fully elapsed (>= day 90, where the stepped
		// relief already reaches "unlimited"). Re-activating sooner keeps the original started_at so the
		// stepped relief keeps progressing — governance cannot indefinitely restart the halt by toggling
		// Active. An already-active update (cap change) likewise preserves the window.
		if em.StartedAt == 0 || (!em.Active && now-em.StartedAt >= types.EmergencyDay90) {
			em.StartedAt = now
		}
		em.Active = true
		em.CapBeforeDay30 = msg.CapBeforeDay30 // "" = halted before day 30
		em.CapFromDay30 = defaultIfEmpty(msg.CapFromDay30, types.DefaultEmergencyCapFromDay30Toman)
		em.CapFromDay60 = defaultIfEmpty(msg.CapFromDay60, types.DefaultEmergencyCapFromDay60Toman)
	} else {
		em.Active = false // deactivate; the record is retained but no longer enforced
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
