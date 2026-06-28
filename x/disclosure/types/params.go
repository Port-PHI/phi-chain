// SPDX-License-Identifier: Apache-2.0

package types

import "fmt"

// DefaultMaxProofSizeBytes bounds the proof blob a verification query accepts.
const DefaultMaxProofSizeBytes = uint32(16384) // 16 KiB

// DefaultParams returns the default module parameters.
func DefaultParams() Params {
	return Params{
		MaxProofSizeBytes: DefaultMaxProofSizeBytes,
	}
}

// Validate checks the parameters.
func (p Params) Validate() error {
	if p.MaxProofSizeBytes == 0 {
		return fmt.Errorf("max_proof_size_bytes must be > 0")
	}
	return nil
}
