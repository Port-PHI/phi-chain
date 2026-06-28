// SPDX-License-Identifier: Apache-2.0

package types

import "fmt"

// Default parameter values.
const (
	// DefaultMaxOptions caps the number of choices per election.
	DefaultMaxOptions = uint32(32)
	// DefaultMinVotingPeriodSeconds is the shortest allowed voting window — 1 hour.
	DefaultMinVotingPeriodSeconds = int64(60 * 60)
	// DefaultMaxProofSizeBytes bounds the eligibility proof blob — 16 KiB.
	DefaultMaxProofSizeBytes = uint32(16384)
)

// DefaultParams returns the default module parameters.
func DefaultParams() Params {
	return Params{
		MaxOptions:             DefaultMaxOptions,
		MinVotingPeriodSeconds: DefaultMinVotingPeriodSeconds,
		MaxProofSizeBytes:      DefaultMaxProofSizeBytes,
	}
}

// Validate checks the parameters.
func (p Params) Validate() error {
	if p.MaxOptions < 2 {
		return fmt.Errorf("max_options must be >= 2")
	}
	if p.MinVotingPeriodSeconds < 0 {
		return fmt.Errorf("min_voting_period_seconds must be >= 0")
	}
	if p.MaxProofSizeBytes == 0 {
		return fmt.Errorf("max_proof_size_bytes must be > 0")
	}
	return nil
}
