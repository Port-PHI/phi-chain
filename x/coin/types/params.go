// SPDX-License-Identifier: Apache-2.0

package types

import (
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	DefaultTransferFee             = "5000"   // 0.005 PHI
	DefaultAnchorPersonalFee       = "120000" // 0.12 PHI
	DefaultCreateAgreementFee      = "50000"  // 0.05 PHI base + per-extra-signer surcharge
	DefaultAgreementExtraSignerFee = "5000"   // 0.005 PHI per extra signer
	// DefaultAgreementFreeSigners is the count of required signers carrying no surcharge.
	DefaultAgreementFreeSigners    = uint32(5)
	DefaultMicroThreshold          = "50000" // 0.05 PHI
	DefaultMicroDailyQuota         = 10
	DefaultCoinAgeThresholdSeconds = int64(7 * 24 * 60 * 60)
	DefaultYoungPenaltyBps         = 100 // early-redeem penalty on young coin
	DefaultOldPenaltyBps           = 20  // early-redeem penalty on old coin
	// DefaultMaxCoinAgeLots bounds the FIFO coin-age queue; on overflow the two oldest lots merge (anti-dust).
	DefaultMaxCoinAgeLots = uint32(64)
	// MinCoinAgeLots is the floor: below two lots there is nothing to merge.
	MinCoinAgeLots = uint32(2)
	// MaxCoinAgeLotsCeiling bounds the governed bound itself.
	MaxCoinAgeLotsCeiling = uint32(1024)

	// MaxMicroThresholdUphi caps the micro-exemption threshold at 1 PHI.
	MaxMicroThresholdUphi = int64(1_000_000)
	// MaxMicroDailyQuota caps the per-DID daily micro-exemption count.
	MaxMicroDailyQuota = uint64(1000)

	MsgTransferTypeURL = "/phi.coin.MsgTransfer"
	// Credential-module anchors, spelled out to keep the fee path free of a module dependency.
	MsgAnchorCredentialTypeURL = "/phi.credentials.MsgAnchorCredential"
	MsgAnchorPersonalTypeURL   = "/phi.credentials.MsgAnchorPersonal"
	// MsgCreateAgreementTypeURL is the only message whose fee depends on its own contents.
	MsgCreateAgreementTypeURL = "/phi.credentials.MsgCreateAgreement"

	BpsDenominator = 10_000
	// MaxProtocolFeeReserveBps mirrors x/institutions' MaxProtocolFeeBps (10%); the penalty bound reserves room for the protocol fee carved out of the same redemption.
	MaxProtocolFeeReserveBps = 1_000

	// DefaultTransferValidatorBps / DefaultTransferCompanyBps split the transfer fee 90/10.
	DefaultTransferValidatorBps = 9_000
	DefaultTransferCompanyBps   = 1_000
)

// DefaultParams returns the default economic params.
func DefaultParams() Params {
	return Params{
		Fees: []FeeEntry{
			{MsgTypeUrl: MsgTransferTypeURL, Fee: DefaultTransferFee},
			{MsgTypeUrl: MsgAnchorPersonalTypeURL, Fee: DefaultAnchorPersonalFee},
			// Base fee only; the per-extra-signer surcharge is applied by FeeForMsg from the message.
			{MsgTypeUrl: MsgCreateAgreementTypeURL, Fee: DefaultCreateAgreementFee},
		},
		Splits: []SplitEntry{
			{MsgTypeUrl: MsgTransferTypeURL, ValidatorBps: DefaultTransferValidatorBps, CompanyBps: DefaultTransferCompanyBps},
			// Personal anchor is a pure protocol service — 100% company.
			{MsgTypeUrl: MsgAnchorPersonalTypeURL, ValidatorBps: 0, CompanyBps: BpsDenominator},
			// A credential anchor is deliberately absent: it pays the ordinary action fee to the validator.
		},
		DefaultFee:              DefaultTransferFee,
		MicroThreshold:          DefaultMicroThreshold,
		MicroDailyQuota:         DefaultMicroDailyQuota,
		CoinAgeThresholdSeconds: DefaultCoinAgeThresholdSeconds,
		YoungPenaltyBps:         DefaultYoungPenaltyBps,
		OldPenaltyBps:           DefaultOldPenaltyBps,
		MaxCoinAgeLots:          DefaultMaxCoinAgeLots,
		// Empty at genesis: set by governance before first withdrawal; empty means withdrawals refused.
		CompanyPayoutAddress:    "",
		AgreementExtraSignerFee: DefaultAgreementExtraSignerFee,
		AgreementFreeSigners:    DefaultAgreementFreeSigners,
	}
}

// Validate checks the params for correctness.
func (p Params) Validate() error {
	// default_fee is the fallback anti-spam fee; it must be strictly positive.
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
	// Empty means no surcharge; otherwise it must parse and be non-negative.
	if p.AgreementExtraSignerFee != "" {
		v, ok := math.NewIntFromString(p.AgreementExtraSignerFee)
		if !ok || v.IsNegative() {
			return fmt.Errorf("invalid agreement_extra_signer_fee: %q", p.AgreementExtraSignerFee)
		}
	}
	if p.CoinAgeThresholdSeconds < 0 {
		return fmt.Errorf("coin_age_threshold_seconds must be >= 0")
	}
	// Penalties are carved from a redemption before the burn and must reserve room for the protocol fee carved from the same redemption, so a positive fraction always stays burnable (always-open redemption).
	if uint64(p.YoungPenaltyBps)+uint64(p.OldPenaltyBps) >= BpsDenominator-MaxProtocolFeeReserveBps {
		return fmt.Errorf("young_penalty_bps + old_penalty_bps (%d + %d) must be < %d, reserving %d bps for the protocol fee so a redemption always returns value",
			p.YoungPenaltyBps, p.OldPenaltyBps, BpsDenominator-MaxProtocolFeeReserveBps, MaxProtocolFeeReserveBps)
	}
	if p.MaxCoinAgeLots < MinCoinAgeLots || p.MaxCoinAgeLots > MaxCoinAgeLotsCeiling {
		return fmt.Errorf("max_coin_age_lots must be in [%d, %d], got %d",
			MinCoinAgeLots, MaxCoinAgeLotsCeiling, p.MaxCoinAgeLots)
	}
	seenSplit := make(map[string]bool, len(p.Splits))
	for _, s := range p.Splits {
		if s.MsgTypeUrl == "" {
			return fmt.Errorf("split entry with empty msg_type_url")
		}
		// Routing must not depend on table order, so reject duplicates.
		if seenSplit[s.MsgTypeUrl] {
			return fmt.Errorf("duplicate split entry for %s", s.MsgTypeUrl)
		}
		seenSplit[s.MsgTypeUrl] = true
		// Widen before adding so a near-max uint32 pair cannot wrap and pass an over-100% split.
		if uint64(s.ValidatorBps)+uint64(s.CompanyBps) > BpsDenominator {
			return fmt.Errorf("split for %s: validator_bps + company_bps must be <= %d, got %d+%d",
				s.MsgTypeUrl, BpsDenominator, s.ValidatorBps, s.CompanyBps)
		}
	}
	// Empty means withdrawals disabled; a non-empty value must be a valid address.
	if p.CompanyPayoutAddress != "" {
		if _, err := sdk.AccAddressFromBech32(p.CompanyPayoutAddress); err != nil {
			return fmt.Errorf("invalid company_payout_address %q: %w", p.CompanyPayoutAddress, err)
		}
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

type agreementMsg interface {
	GetRequiredSigners() []string
}

// FeeForMsg returns the base table fee plus any content-dependent surcharge; a pure function of (params, message) so every validator computes the same fee (ante-safe).
func (p Params) FeeForMsg(msg sdk.Msg) math.Int {
	url := sdk.MsgTypeURL(msg)
	return p.FeeFor(url).Add(p.agreementSurcharge(url, msg))
}

func (p Params) agreementSurcharge(msgTypeURL string, msg sdk.Msg) math.Int {
	if msgTypeURL != MsgCreateAgreementTypeURL {
		return math.ZeroInt()
	}
	a, ok := msg.(agreementMsg)
	if !ok {
		return math.ZeroInt()
	}
	signers := int64(len(a.GetRequiredSigners()))
	free := int64(p.AgreementFreeSigners)
	if signers <= free {
		return math.ZeroInt()
	}
	// An unset/unparsable surcharge means NO surcharge (fee-favourable to the payer), never a panic and never a silent fallback to some other number.
	extra, ok := math.NewIntFromString(p.AgreementExtraSignerFee)
	if !ok || !extra.IsPositive() {
		return math.ZeroInt()
	}
	return extra.MulRaw(signers - free)
}

// SplitFor returns the revenue split for a message type, and whether the table actually carries an entry for it.
func (p Params) SplitFor(msgTypeURL string) (SplitEntry, bool) {
	for _, s := range p.Splits {
		if s.MsgTypeUrl == msgTypeURL {
			return s, true
		}
	}
	return SplitEntry{MsgTypeUrl: msgTypeURL, ValidatorBps: BpsDenominator, CompanyBps: 0}, false
}

// CompanyShare returns the company's cut of `fee` under this entry, and the validator's cut as the REMAINDER (fee − company).
func (s SplitEntry) CompanyShare(fee math.Int) (company, validator math.Int) {
	company = fee.MulRaw(int64(s.CompanyBps)).QuoRaw(BpsDenominator)
	return company, fee.Sub(company)
}

// MicroThresholdInt returns the micro-exemption threshold as an Int.
func (p Params) MicroThresholdInt() math.Int {
	if v, ok := math.NewIntFromString(p.MicroThreshold); ok {
		return v
	}
	return math.ZeroInt()
}
