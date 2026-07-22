// SPDX-License-Identifier: Apache-2.0

package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParamsValidate_FeeAndThresholdBounds covers the case where the fallback default_fee must be strictly positive (a zero/negative fee removes the only anti-spam cost), micro_threshold must be non-negative and bounded, and micro_daily_quota must be bounded.
func TestParamsValidate_FeeAndThresholdBounds(t *testing.T) {
	require.NoError(t, DefaultParams().Validate(), "default params must validate")

	cases := []struct {
		name    string
		mutate  func(*Params)
		wantErr bool
	}{
		{"zero default_fee rejected", func(p *Params) { p.DefaultFee = "0" }, true},
		{"negative default_fee rejected", func(p *Params) { p.DefaultFee = "-5000" }, true},
		{"unparsable default_fee rejected", func(p *Params) { p.DefaultFee = "abc" }, true},
		{"positive default_fee ok", func(p *Params) { p.DefaultFee = "1" }, false},
		{"negative micro_threshold rejected", func(p *Params) { p.MicroThreshold = "-1" }, true},
		{"zero micro_threshold ok (no exemption)", func(p *Params) { p.MicroThreshold = "0" }, false},
		{"over-cap micro_threshold rejected", func(p *Params) { p.MicroThreshold = "1000001" }, true},
		{"over-cap micro_daily_quota rejected", func(p *Params) { p.MicroDailyQuota = MaxMicroDailyQuota + 1 }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := DefaultParams()
			tc.mutate(&p)
			if tc.wantErr {
				require.Error(t, p.Validate())
			} else {
				require.NoError(t, p.Validate())
			}
		})
	}
}

// The split table is consensus-critical routing: an over-100% row, a duplicate row (which would make routing depend on table order) and a malformed payout address must all be refused by validation, not discovered when a fee is being routed.
func TestParamsValidate_SplitTableBounds(t *testing.T) {
	require.NoError(t, DefaultParams().Validate(), "the default split table must validate")

	cases := []struct {
		name    string
		mutate  func(*Params)
		wantErr bool
	}{
		{"bps sum over 10000 rejected", func(p *Params) {
			p.Splits = []SplitEntry{{MsgTypeUrl: MsgTransferTypeURL, ValidatorBps: 9_000, CompanyBps: 1_001}}
		}, true},
		{"bps sum at 10000 ok", func(p *Params) {
			p.Splits = []SplitEntry{{MsgTypeUrl: MsgTransferTypeURL, ValidatorBps: 9_000, CompanyBps: 1_000}}
		}, false},
		{"bps sum under 10000 ok (the shortfall accrues to the validator leg)", func(p *Params) {
			p.Splits = []SplitEntry{{MsgTypeUrl: MsgTransferTypeURL, ValidatorBps: 1_000, CompanyBps: 1_000}}
		}, false},
		{"uint32 wraparound cannot smuggle an over-100% split", func(p *Params) {
			p.Splits = []SplitEntry{{MsgTypeUrl: MsgTransferTypeURL, ValidatorBps: 4_294_967_295, CompanyBps: 10_001}}
		}, true},
		{"empty msg_type_url rejected", func(p *Params) {
			p.Splits = []SplitEntry{{MsgTypeUrl: "", CompanyBps: 1_000}}
		}, true},
		{"duplicate msg_type_url rejected", func(p *Params) {
			p.Splits = []SplitEntry{
				{MsgTypeUrl: MsgTransferTypeURL, ValidatorBps: 9_000, CompanyBps: 1_000},
				{MsgTypeUrl: MsgTransferTypeURL, ValidatorBps: 0, CompanyBps: 10_000},
			}
		}, true},
		{"empty split table ok (every fee stays flat to the fee collector)", func(p *Params) {
			p.Splits = nil
		}, false},
		{"malformed company_payout_address rejected", func(p *Params) {
			p.CompanyPayoutAddress = "phi1notanaddress"
		}, true},
		{"empty company_payout_address ok (withdrawals disabled)", func(p *Params) {
			p.CompanyPayoutAddress = ""
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := DefaultParams()
			tc.mutate(&p)
			if tc.wantErr {
				require.Error(t, p.Validate())
			} else {
				require.NoError(t, p.Validate())
			}
		})
	}
}

// The default table encodes the settled routing: transfer 90/10, personal anchor 100% company, and a credential anchor charged NET (its whole fee is the protocol's own cut).
func TestDefaultSplits_EncodeTheSettledRouting(t *testing.T) {
	p := DefaultParams()

	transfer, found := p.SplitFor(MsgTransferTypeURL)
	require.True(t, found)
	require.Equal(t, uint32(9_000), transfer.ValidatorBps)
	require.Equal(t, uint32(1_000), transfer.CompanyBps)

	personal, found := p.SplitFor(MsgAnchorPersonalTypeURL)
	require.True(t, found, "the personal anchor must be in the default split table")
	require.Equal(t, uint32(0), personal.ValidatorBps, "a personal anchor pays the validators nothing")
	require.Equal(t, uint32(BpsDenominator), personal.CompanyBps, "a personal anchor is 100% company revenue")

	cert, found := p.SplitFor(MsgAnchorCredentialTypeURL)
	require.False(t, found, "the credential anchor must NOT carry a split entry")
	require.Equal(t, uint32(BpsDenominator), cert.ValidatorBps, "the whole cert-anchor fee goes to the validators")
	require.Equal(t, uint32(0), cert.CompanyBps, "a credential anchor must route nothing to phi_revenue")

	fallback, found := p.SplitFor("/phi.coin.MsgSomethingElse")
	require.False(t, found)
	require.Equal(t, uint32(BpsDenominator), fallback.ValidatorBps)
	require.Equal(t, uint32(0), fallback.CompanyBps)
}

// The surcharge rate is a fee rate like any other: malformed or negative must be refused by validation, and empty must be read as "no surcharge" rather than rejected.
func TestParamsValidate_AgreementSurchargeRate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Params)
		wantErr bool
	}{
		{"unparsable surcharge rejected", func(p *Params) { p.AgreementExtraSignerFee = "abc" }, true},
		{"negative surcharge rejected", func(p *Params) { p.AgreementExtraSignerFee = "-5000" }, true},
		{"zero surcharge ok (disabled)", func(p *Params) { p.AgreementExtraSignerFee = "0" }, false},
		{"empty surcharge ok (disabled)", func(p *Params) { p.AgreementExtraSignerFee = "" }, false},
		{"zero free signers ok (every signer charged)", func(p *Params) { p.AgreementFreeSigners = 0 }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := DefaultParams()
			tc.mutate(&p)
			if tc.wantErr {
				require.Error(t, p.Validate())
			} else {
				require.NoError(t, p.Validate())
			}
		})
	}
}

// The early-redeem penalty is carved out of a redemption BEFORE the burn, so the two coin-age tiers together must leave something to burn: a redemption must always return value, never carve 100% and hand the holder nothing (ErrNothingRedeemed).
func TestParamsValidate_RedeemPenaltyLeavesValue(t *testing.T) {
	require.NoError(t, DefaultParams().Validate(), "the default stepped penalty must validate")

	const headroom = BpsDenominator - MaxProtocolFeeReserveBps // 9,000 bps

	cases := []struct {
		name    string
		mutate  func(*Params)
		wantErr bool
	}{
		{"100% young penalty rejected", func(p *Params) { p.YoungPenaltyBps = BpsDenominator }, true},
		{"100% old penalty rejected", func(p *Params) { p.OldPenaltyBps = BpsDenominator }, true},
		{"tiers summing to 100% rejected", func(p *Params) { p.YoungPenaltyBps = 6000; p.OldPenaltyBps = 4000 }, true},
		{"tiers summing above 100% rejected", func(p *Params) { p.YoungPenaltyBps = 9000; p.OldPenaltyBps = 2000 }, true},
		{"tiers leaving the fee no room rejected", func(p *Params) { p.YoungPenaltyBps = 8000; p.OldPenaltyBps = 1999 }, true},
		{"tiers exactly consuming the headroom rejected", func(p *Params) { p.YoungPenaltyBps = 8000; p.OldPenaltyBps = 1000 }, true},
		{"tiers one bp inside the headroom ok", func(p *Params) { p.YoungPenaltyBps = 8000; p.OldPenaltyBps = 999 }, false},
		{"the default stepped penalty ok", func(p *Params) { p.YoungPenaltyBps = 100; p.OldPenaltyBps = 20 }, false},
	}
	require.Equal(t, 9_000, headroom, "the reserved headroom is 90% of a redemption")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := DefaultParams()
			tc.mutate(&p)
			if tc.wantErr {
				require.Error(t, p.Validate())
			} else {
				require.NoError(t, p.Validate())
			}
		})
	}
}

// The default fee table seeds exactly the settled schedule, and nothing else moved.
func TestDefaultFees_EncodeTheSettledSchedule(t *testing.T) {
	p := DefaultParams()

	require.Equal(t, "5000", p.FeeFor(MsgTransferTypeURL).String(), "transfer: 0.005 PHI (unchanged)")
	require.Equal(t, "120000", p.FeeFor(MsgAnchorPersonalTypeURL).String(), "personal anchor: 0.12 PHI")
	require.Equal(t, "50000", p.FeeFor(MsgCreateAgreementTypeURL).String(), "agreement base: 0.05 PHI")
	require.Equal(t, "5000", p.FeeFor(MsgAnchorCredentialTypeURL).String(),
		"the cert anchor still pays default_fee: its amount is a separate decision")
	require.Equal(t, DefaultAgreementExtraSignerFee, p.AgreementExtraSignerFee)
	require.Equal(t, uint32(5), p.AgreementFreeSigners, "the 6th signer is the first charged")
}
