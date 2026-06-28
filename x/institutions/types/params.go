// SPDX-License-Identifier: Apache-2.0

package types

import (
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
)

// DefaultPhiToToman is the canonical fixed rate: 1 PHI = 100,000 toman.
const DefaultPhiToToman = uint64(100_000)

// DefaultSensitiveThreshold is the default multisig threshold for sensitive actions (2 of N).
const DefaultSensitiveThreshold = uint32(2)

// Emergency stepped-redemption day thresholds (seconds) and default per-holder cumulative caps
// (Toman): before day 30 → halted; from day 30 → 200 PHI = 20,000,000 Toman; from day 60 →
// 2,000 PHI = 200,000,000 Toman; from day 90 → unlimited.
const (
	EmergencyDay30 = int64(30 * 86400)
	EmergencyDay60 = int64(60 * 86400)
	EmergencyDay90 = int64(90 * 86400)

	DefaultEmergencyCapFromDay30Toman = "20000000"  // 200 PHI
	DefaultEmergencyCapFromDay60Toman = "200000000" // 2000 PHI
)

// DefaultParams returns the default params.
// operator is set in genesis (empty default = no direct add allowed).
func DefaultParams() Params {
	return Params{
		Operator:         "",
		PhiToToman:       DefaultPhiToToman,
		RedeemFloorPerTx: "",
	}
}

// Validate checks the validity of the module params.
func (p Params) Validate() error {
	if p.PhiToToman == 0 {
		return fmt.Errorf("phi_to_toman must be > 0")
	}
	// phi_to_toman must divide UphiPerPhi so the toman→uphi conversion (uphi = toman·UphiPerPhi /
	// phi_to_toman) is always integral. A non-divisor would make some mints non-integral and could halt
	// the mint rail (ErrNonIntegralMint) or, if mis-handled, desynchronize supply and vaults. Governance
	// can change phi_to_toman only while all vaults are empty; this keeps the rate sane regardless.
	if cointypes.UphiPerPhi%p.PhiToToman != 0 {
		return fmt.Errorf("phi_to_toman (%d) must divide UphiPerPhi (%d) so toman→uphi conversion is integral", p.PhiToToman, cointypes.UphiPerPhi)
	}
	if p.Operator != "" {
		if _, err := sdk.AccAddressFromBech32(p.Operator); err != nil {
			return fmt.Errorf("invalid operator address: %w", err)
		}
	}
	if p.PenaltyDestination != "" {
		if _, err := sdk.AccAddressFromBech32(p.PenaltyDestination); err != nil {
			return fmt.Errorf("invalid penalty_destination address: %w", err)
		}
	}
	if err := validateNonNegToman("redeem_floor_per_tx", p.RedeemFloorPerTx); err != nil {
		return err
	}
	if err := validateNonNegToman("large_mint_threshold_toman", p.LargeMintThresholdToman); err != nil {
		return err
	}
	if err := validateNonNegToman("mint_ceiling_per_tx", p.MintCeilingPerTx); err != nil {
		return err
	}
	if err := validateNonNegToman("mint_ceiling_daily", p.MintCeilingDaily); err != nil {
		return err
	}
	if err := p.EmergencyRedemption.Validate(); err != nil {
		return err
	}
	return nil
}

// Validate checks the emergency redemption parameters (caps non-negative; started_at sane when active).
func (e EmergencyRedemption) Validate() error {
	for name, v := range map[string]string{
		"cap_before_day30": e.CapBeforeDay30, "cap_from_day30": e.CapFromDay30, "cap_from_day60": e.CapFromDay60,
	} {
		if err := validateNonNegToman(name, v); err != nil {
			return err
		}
	}
	if e.Active && e.StartedAt < 0 {
		return fmt.Errorf("emergency_redemption.started_at must not be negative")
	}
	return nil
}

// CapInt converts a cap string to math.Int; "", "0", or invalid = zero (no cap).
func CapInt(s string) math.Int {
	if s == "" {
		return math.ZeroInt()
	}
	if v, ok := math.NewIntFromString(s); ok && !v.IsNegative() {
		return v
	}
	return math.ZeroInt()
}

// Validate checks the structural validity of InstitutionParams (all caps non-negative; threshold ≥ 0).
// The "stricter only" rule relative to the protocol floor is checked in the keeper (which has access to the module Params).
func (p InstitutionParams) Validate() error {
	c := p.Caps
	for name, v := range map[string]string{
		"mint_daily": c.MintDaily, "mint_per_tx": c.MintPerTx, "mint_per_user": c.MintPerUser,
		"redeem_daily": c.RedeemDaily, "redeem_per_tx": c.RedeemPerTx, "redeem_per_user": c.RedeemPerUser,
	} {
		if err := validateNonNegToman(name, v); err != nil {
			return err
		}
	}
	for _, kt := range p.KycTierLimits {
		if err := validateNonNegToman(fmt.Sprintf("kyc_tier_limit[%d]", kt.Tier), kt.DailyLimitToman); err != nil {
			return err
		}
	}
	return nil
}

// validateNonNegToman: an empty string is allowed (meaning unset); otherwise it must be a non-negative integer.
func validateNonNegToman(field, s string) error {
	if s == "" {
		return nil
	}
	v, ok := math.NewIntFromString(s)
	if !ok {
		return fmt.Errorf("%s: invalid integer %q", field, s)
	}
	if v.IsNegative() {
		return fmt.Errorf("%s: must not be negative", field)
	}
	return nil
}
