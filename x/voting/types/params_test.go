// SPDX-License-Identifier: Apache-2.0

package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// max_proof_size_bytes is bounded on both ends: zero and above-ceiling are rejected.
func TestParamsValidate_MaxProofSizeBounds(t *testing.T) {
	require.NoError(t, DefaultParams().Validate())

	p := DefaultParams()
	p.MaxProofSizeBytes = MaxProofSizeBytesCeiling
	require.NoError(t, p.Validate(), "a value exactly at the ceiling is allowed")

	p.MaxProofSizeBytes = MaxProofSizeBytesCeiling + 1
	require.Error(t, p.Validate(), "a value above the ceiling must be rejected")

	p.MaxProofSizeBytes = 0
	require.Error(t, p.Validate(), "zero is still rejected")
}

// max_voting_period_seconds: 0 (disabled) or >= min_voting_period_seconds; negative rejected.
func TestParamsValidate_MaxVotingPeriod(t *testing.T) {
	p := DefaultParams()
	p.MaxVotingPeriodSeconds = 0
	require.NoError(t, p.Validate(), "0 disables the upper bound")

	p.MaxVotingPeriodSeconds = -1
	require.Error(t, p.Validate(), "a negative max period is rejected")

	p.MaxVotingPeriodSeconds = p.MinVotingPeriodSeconds - 1
	require.Error(t, p.Validate(), "a max below the min leaves no valid window")

	p.MaxVotingPeriodSeconds = p.MinVotingPeriodSeconds
	require.NoError(t, p.Validate(), "max == min is allowed")
}

// The protobuf codec must round-trip max_voting_period_seconds.
func TestParams_ProtoRoundTrip(t *testing.T) {
	in := Params{
		MaxOptions:             7,
		MinVotingPeriodSeconds: 3600,
		MaxProofSizeBytes:      12345,
		MaxVotingPeriodSeconds: 987654,
	}
	bz, err := in.Marshal()
	require.NoError(t, err)

	var out Params
	require.NoError(t, out.Unmarshal(bz))
	require.Equal(t, in, out)
	require.Equal(t, int64(987654), out.GetMaxVotingPeriodSeconds())
}
