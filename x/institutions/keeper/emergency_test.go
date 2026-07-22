// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

func (f fixture) at(days int64) sdk.Context {
	return f.ctx.WithBlockTime(f.ctx.BlockTime().Add(time.Duration(days) * 24 * time.Hour))
}

func (f fixture) redeemAt(days int64, amountToman, ref string) error {
	_, err := f.msg.InstitutionRedeem(f.at(days), &types.MsgInstitutionRedeem{
		Admin: f.holder.String(), Institution: "bank-a", Holder: f.holder.String(), AmountToman: amountToman, RedeemRef: ref,
	})
	return err
}

// TestEmergencyRedemption_NoResetWithinWindow covers the case where governance cannot indefinitely restart the redemption halt by toggling Active — re-activation within the day-90 window keeps the original started_at, so the stepped relief keeps progressing; only after the window may it reset.
func TestEmergencyRedemption_NoResetWithinWindow(t *testing.T) {
	f := setup(t)
	t0 := time.Unix(2_000_000_000, 0)
	started := func(ctx sdk.Context) int64 { return f.k.GetParams(ctx).EmergencyRedemption.StartedAt }
	set := func(ctx sdk.Context, active bool) {
		_, err := f.msg.SetEmergencyRedemption(ctx, &types.MsgSetEmergencyRedemption{Authority: f.authority, Active: active})
		require.NoError(t, err)
	}

	c0 := f.ctx.WithBlockTime(t0)
	set(c0, true)
	require.Equal(t, t0.Unix(), started(c0))

	cMid := f.ctx.WithBlockTime(t0.Add(10 * 24 * time.Hour))
	set(cMid, false)
	set(cMid, true)
	require.Equal(t, t0.Unix(), started(cMid), "re-activation within the window must not restart the halt")

	cLate := f.ctx.WithBlockTime(t0.Add(100 * 24 * time.Hour))
	set(cLate, false)
	set(cLate, true)
	require.Equal(t, t0.Add(100*24*time.Hour).Unix(), started(cLate), "a fresh emergency after the window resets started_at")
}

// The stepped brake relaxes over time: halted before day 30, 200 PHI from day 30, 2,000 PHI from day 60 (both cumulative per holder), unlimited from day 90.
func TestEmergencyRedemption_SteppedCaps(t *testing.T) {
	f := setup(t)
	f.ctx = f.ctx.WithBlockTime(time.Unix(1_700_000_000, 0))
	f.registerAndAttest(t, "bank-a", 1_000_000_000) // 1e9 Toman reserve
	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "1000000000", DepositRef: "dep-1",
	})
	require.NoError(t, err)

	_, err = f.msg.SetEmergencyRedemption(f.ctx, &types.MsgSetEmergencyRedemption{Authority: f.authority, Active: true})
	require.NoError(t, err)

	require.ErrorIs(t, f.redeemAt(1, "1", "red-1"), types.ErrRedemptionThrottled)

	require.NoError(t, f.redeemAt(35, "20000000", "red-2"))
	require.ErrorIs(t, f.redeemAt(35, "1", "red-3"), types.ErrRedemptionThrottled) // cumulative over the cap

	require.NoError(t, f.redeemAt(65, "180000000", "red-4"))
	require.ErrorIs(t, f.redeemAt(65, "1", "red-5"), types.ErrRedemptionThrottled)

	require.NoError(t, f.redeemAt(95, "300000000", "red-6"))

	inst, _ := f.k.GetInstitution(f.ctx, "bank-a")
	require.Equal(t, "500000000", inst.VaultBalance)
}

// Deactivating the brake removes the throttle entirely.
func TestEmergencyRedemption_DeactivateLiftsThrottle(t *testing.T) {
	f := setup(t)
	f.ctx = f.ctx.WithBlockTime(time.Unix(1_700_000_000, 0))
	f.registerAndAttest(t, "bank-a", 1_000_000_000)
	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "1000000000", DepositRef: "dep-1",
	})
	require.NoError(t, err)

	_, err = f.msg.SetEmergencyRedemption(f.ctx, &types.MsgSetEmergencyRedemption{Authority: f.authority, Active: true})
	require.NoError(t, err)
	require.ErrorIs(t, f.redeemAt(1, "1000", "red-1"), types.ErrRedemptionThrottled)

	_, err = f.msg.SetEmergencyRedemption(f.ctx, &types.MsgSetEmergencyRedemption{Authority: f.authority, Active: false})
	require.NoError(t, err)
	require.NoError(t, f.redeemAt(1, "500000000", "red-2"))
}

// Only the governance authority may toggle the brake.
func TestEmergencyRedemption_OnlyAuthority(t *testing.T) {
	f := setup(t)
	_, err := f.msg.SetEmergencyRedemption(f.ctx, &types.MsgSetEmergencyRedemption{Authority: f.oper.String(), Active: true})
	require.Error(t, err)

	_, err = f.msg.SetEmergencyRedemption(f.ctx, &types.MsgSetEmergencyRedemption{Authority: f.authority, Active: true})
	require.NoError(t, err)
	p := f.k.GetParams(f.ctx)
	require.True(t, p.EmergencyRedemption.Active)
	require.Equal(t, types.DefaultEmergencyCapFromDay30Toman, p.EmergencyRedemption.CapFromDay30)
	require.Equal(t, types.DefaultEmergencyCapFromDay60Toman, p.EmergencyRedemption.CapFromDay60)
}
