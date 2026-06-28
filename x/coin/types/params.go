// SPDX-License-Identifier: Apache-2.0

package types

import (
	"fmt"

	"cosmossdk.io/math"
)

// Default fee and coin-age values (in uphi and bps).
const (
	// DefaultTransferFee is the transfer fee: 0.005 PHI = 5,000 uphi.
	DefaultTransferFee = "5000"
	// DefaultMicroThreshold is the micro-exemption threshold: 0.05 PHI = 50,000 uphi.
	DefaultMicroThreshold = "50000"
	// DefaultMicroDailyQuota is the per-DID daily micro-exemption quota.
	DefaultMicroDailyQuota = 10
	// DefaultCoinAgeThresholdSeconds is the coin-age threshold: 7 days.
	DefaultCoinAgeThresholdSeconds = int64(7 * 24 * 60 * 60)
	// DefaultYoungBurnBps is the young-coin burn: 1% = 100 bps.
	DefaultYoungBurnBps = 100
	// DefaultOldBurnBps is the old-coin burn: 0.2% = 20 bps.
	DefaultOldBurnBps = 20

	// MaxMicroThresholdUphi caps the micro-exemption threshold: above 1 PHI (1,000,000
	// uphi) a transfer is not "micro" and would erode the anti-spam fee. Generous (20x the default).
	MaxMicroThresholdUphi = int64(1_000_000)
	// MaxMicroDailyQuota caps the per-DID daily micro-exemption count. Generous (100x default).
	MaxMicroDailyQuota = uint64(1000)

	// MsgTransferTypeURL is the transfer message type (used by the fee table).
	MsgTransferTypeURL = "/phi.coin.MsgTransfer"
)

// DefaultParams returns the default economic params.
func DefaultParams() Params {
	return Params{
		Fees: []FeeEntry{
			{MsgTypeUrl: MsgTransferTypeURL, Fee: DefaultTransferFee},
		},
		DefaultFee:              DefaultTransferFee,
		MicroThreshold:          DefaultMicroThreshold,
		MicroDailyQuota:         DefaultMicroDailyQuota,
		CoinAgeThresholdSeconds: DefaultCoinAgeThresholdSeconds,
		YoungBurnBps:            DefaultYoungBurnBps,
		OldBurnBps:              DefaultOldBurnBps,
	}
}

// Validate checks the params for correctness.
func (p Params) Validate() error {
	// default_fee is the fallback anti-spam fee (used when no per-message entry matches). A zero or
	// negative value would make every such message free, removing the only spam cost.
	fee, ok := math.NewIntFromString(p.DefaultFee)
	if !ok {
		return fmt.Errorf("invalid default_fee: %q", p.DefaultFee)
	}
	if !fee.IsPositive() {
		return fmt.Errorf("default_fee must be strictly positive, got %q", p.DefaultFee)
	}
	mt, ok := math.NewIntFromString(p.MicroThreshold)
	if !ok {
		return fmt.Errorf("invalid micro_threshold: %q", p.MicroThreshold)
	}
	if mt.IsNegative() {
		return fmt.Errorf("micro_threshold must be >= 0, got %q", p.MicroThreshold)
	}
	if mt.GT(math.NewInt(MaxMicroThresholdUphi)) {
		return fmt.Errorf("micro_threshold %q exceeds the sanity cap %d uphi", p.MicroThreshold, MaxMicroThresholdUphi)
	}
	if p.MicroDailyQuota > MaxMicroDailyQuota {
		return fmt.Errorf("micro_daily_quota %d exceeds the sanity cap %d", p.MicroDailyQuota, MaxMicroDailyQuota)
	}
	for _, f := range p.Fees {
		if f.MsgTypeUrl == "" {
			return fmt.Errorf("fee entry with empty msg_type_url")
		}
		v, ok := math.NewIntFromString(f.Fee)
		if !ok || v.IsNegative() {
			return fmt.Errorf("invalid fee for %s: %q", f.MsgTypeUrl, f.Fee)
		}
	}
	if p.CoinAgeThresholdSeconds < 0 {
		return fmt.Errorf("coin_age_threshold_seconds must be >= 0")
	}
	if p.YoungBurnBps > 10_000 || p.OldBurnBps > 10_000 {
		return fmt.Errorf("burn bps must be <= 10000")
	}
	return nil
}

// FeeFor returns the fixed fee for a message type (falling back to default_fee when no entry matches).
func (p Params) FeeFor(msgTypeURL string) math.Int {
	for _, f := range p.Fees {
		if f.MsgTypeUrl == msgTypeURL {
			if v, ok := math.NewIntFromString(f.Fee); ok {
				return v
			}
		}
	}
	if v, ok := math.NewIntFromString(p.DefaultFee); ok {
		return v
	}
	return math.ZeroInt()
}

// MicroThresholdInt returns the micro-exemption threshold as an Int.
func (p Params) MicroThresholdInt() math.Int {
	if v, ok := math.NewIntFromString(p.MicroThreshold); ok {
		return v
	}
	return math.ZeroInt()
}
