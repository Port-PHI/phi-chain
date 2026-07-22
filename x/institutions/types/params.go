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

// DefaultProtocolFeeBps is the protocol fee on mint and redeem: 0.2% = 20 bps.
const DefaultProtocolFeeBps = uint32(20)

// MaxProtocolFeeBps is the sanity ceiling on the governed protocol fee.
const MaxProtocolFeeBps = uint32(1_000)

// DefaultMaxAttestationStalenessSeconds is the §4.6 attestation-staleness floor: 24 hours.
const DefaultMaxAttestationStalenessSeconds = uint64(24 * 60 * 60)

// MaxAttestationStalenessCeiling bounds the governed floor itself (90 days).
const MaxAttestationStalenessCeiling = uint64(90 * 24 * 60 * 60)

// DefaultRedeemFloorToman is the protocol floor beneath which an institution may not set ANY of its redeem caps: 100,000 Toman, one PHI's worth.
const DefaultRedeemFloorToman = "100000"

// DefaultRedeemDailyCapPerDidUphi is the network-wide daily redemption cap per human: 200 PHI = 200,000,000 uphi.
const DefaultRedeemDailyCapPerDidUphi = "200000000"

// Emergency stepped-redemption day thresholds (seconds) and default per-holder cumulative caps (Toman): before day 30 → halted; from day 30 → 200 PHI = 20,000,000 Toman; from day 60 → 2,000 PHI = 200,000,000 Toman; from day 90 → unlimited.
const (
	EmergencyDay30 = int64(30 * 86400)
	EmergencyDay60 = int64(60 * 86400)
	EmergencyDay90 = int64(90 * 86400)

	DefaultEmergencyCapFromDay30Toman = "20000000"  // 200 PHI
	DefaultEmergencyCapFromDay60Toman = "200000000" // 2000 PHI
)

// DefaultParams returns the default params.
func DefaultParams() Params {
	return Params{
		Operator:                       "",
		PhiToToman:                     DefaultPhiToToman,
		RedeemFloorPerTx:               DefaultRedeemFloorToman,
		RedeemDailyCapPerDidUphi:       DefaultRedeemDailyCapPerDidUphi,
		ProtocolFeeBps:                 DefaultProtocolFeeBps,
		MaxAttestationStalenessSeconds: DefaultMaxAttestationStalenessSeconds,
	}
}

// Validate checks the validity of the module params.
func (p Params) Validate() error {
	if p.PhiToToman == 0 {
		return fmt.Errorf("phi_to_toman must be > 0")
	}
	// phi_to_toman must divide UphiPerPhi so the toman→uphi conversion (uphi = toman·UphiPerPhi / phi_to_toman) is always integral.
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
	// The floor must be POSITIVE.
	if !CapInt(p.RedeemFloorPerTx).IsPositive() {
		return fmt.Errorf("redeem_floor_per_tx must be positive (a zero floor lets an institution strand its holders), got %q",
			p.RedeemFloorPerTx)
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
	// A uphi amount, not Toman — but the same non-negative-integer rule applies.
	if err := validateNonNegToman("redeem_daily_cap_per_did_uphi", p.RedeemDailyCapPerDidUphi); err != nil {
		return err
	}
	// And the same FLOOR applies, which is the part the differing units hid.
	if cap := CapInt(p.RedeemDailyCapPerDidUphi); cap.IsPositive() {
		if floor := p.RedeemFloorUphi(); floor.IsPositive() && cap.LT(floor) {
			return fmt.Errorf(
				"redeem_daily_cap_per_did_uphi %s is below the protocol redeem floor %s uphi (%s Toman); "+
					"a network-wide cap beneath the floor blocks every redemption on the chain",
				cap, floor, p.RedeemFloorPerTx)
		}
	}
	// Bounded, so the carve-out can never consume the customer's whole mint or drive a redemption's burned amount to zero.
	if p.ProtocolFeeBps > MaxProtocolFeeBps {
		return fmt.Errorf("protocol_fee_bps %d exceeds the sanity ceiling %d", p.ProtocolFeeBps, MaxProtocolFeeBps)
	}
	// 0 disables the gate entirely; anything else is bounded, so governance cannot set a "threshold" so long that a stale attestation is effectively never stale.
	if p.MaxAttestationStalenessSeconds > MaxAttestationStalenessCeiling {
		return fmt.Errorf("max_attestation_staleness_seconds %d exceeds the sanity ceiling %d",
			p.MaxAttestationStalenessSeconds, MaxAttestationStalenessCeiling)
	}
	if err := p.EmergencyRedemption.Validate(); err != nil {
		return err
	}
	// The emergency stepped-redemption caps are per-holder Toman caps, floored on the SAME terms as every other redeem cap: a POSITIVE cap below the floor strands holders just as an institution's own cap would.
	if floor := CapInt(p.RedeemFloorPerTx); floor.IsPositive() {
		for _, c := range []struct{ name, value string }{
			{"emergency_redemption.cap_before_day30", p.EmergencyRedemption.CapBeforeDay30},
			{"emergency_redemption.cap_from_day30", p.EmergencyRedemption.CapFromDay30},
			{"emergency_redemption.cap_from_day60", p.EmergencyRedemption.CapFromDay60},
		} {
			if v := CapInt(c.value); v.IsPositive() && v.LT(floor) {
				return fmt.Errorf("%s %s is below the protocol redeem floor %s Toman; "+
					"a positive emergency cap beneath the floor strands every holder", c.name, v, floor)
			}
		}
	}
	return nil
}

// RedeemFloorUphi is the protocol redeem floor expressed in uphi, for the caps denominated that way.
func (p Params) RedeemFloorUphi() math.Int {
	toman := CapInt(p.RedeemFloorPerTx)
	if !toman.IsPositive() || p.PhiToToman == 0 {
		return math.ZeroInt()
	}
	num := toman.Mul(math.NewIntFromUint64(cointypes.UphiPerPhi))
	den := math.NewIntFromUint64(p.PhiToToman)
	if !num.Mod(den).IsZero() {
		return math.ZeroInt()
	}
	return num.Quo(den)
}

// ProtocolFee returns the protocol's cut of `amount` uphi (floor).
func (p Params) ProtocolFee(amount math.Int) math.Int {
	if p.ProtocolFeeBps == 0 {
		return math.ZeroInt()
	}
	return amount.MulRaw(int64(p.ProtocolFeeBps)).QuoRaw(int64(cointypes.BpsDenominator))
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

// AtLeastFloor raises a configured redeem cap to the protocol floor when it sits below it.
func AtLeastFloor(cap, floor math.Int) math.Int {
	if cap.IsPositive() && floor.IsPositive() && cap.LT(floor) {
		return floor
	}
	return cap
}

// RedeemCapsBelowFloor returns the name and value of the FIRST redeem cap in p that is positive and below the protocol floor, or ok=false when every one of them clears it.
func (p InstitutionParams) RedeemCapsBelowFloor(floor math.Int) (name string, value math.Int, ok bool) {
	if !floor.IsPositive() {
		return "", math.ZeroInt(), false
	}
	capped := []struct {
		name string
		cap  string
	}{
		{"redeem_per_tx", p.Caps.RedeemPerTx},
		{"redeem_daily", p.Caps.RedeemDaily},
		{"redeem_per_user", p.Caps.RedeemPerUser},
	}
	// Each configured tier limit is a daily redeem cap in its own right — it is enforced by the same comparison, against the same counter, as redeem_per_user — so it is floored on the same terms.
	for _, kt := range p.KycTierLimits {
		capped = append(capped, struct {
			name string
			cap  string
		}{fmt.Sprintf("kyc_tier_limit[%d]", kt.Tier), kt.DailyLimitToman})
	}
	for _, c := range capped {
		if v := CapInt(c.cap); v.IsPositive() && v.LT(floor) {
			return c.name, v, true
		}
	}
	return "", math.ZeroInt(), false
}

// Validate checks the structural validity of InstitutionParams (all caps non-negative; threshold ≥ 0).
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
