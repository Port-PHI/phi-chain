// SPDX-License-Identifier: Apache-2.0

package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParamsValidate_FeeAndThresholdBounds covers the case where the fallback default_fee must be
// strictly positive (a zero/negative fee removes the only anti-spam cost), micro_threshold must be
// non-negative and bounded, and micro_daily_quota must be bounded.
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
