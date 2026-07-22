// SPDX-License-Identifier: Apache-2.0

package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/disclosure/types"
)

// max_proof_size_bytes must be bounded on BOTH ends: zero is rejected and a value above the ceiling is rejected so governance cannot set a proof size large enough to make verification a denial-of-service vector.
func TestParamsValidate_MaxProofSizeBounds(t *testing.T) {
	require.NoError(t, types.DefaultParams().Validate())

	p := types.DefaultParams()
	p.MaxProofSizeBytes = types.MaxProofSizeBytesCeiling
	require.NoError(t, p.Validate(), "a value exactly at the ceiling is allowed")

	p.MaxProofSizeBytes = types.MaxProofSizeBytesCeiling + 1
	require.Error(t, p.Validate(), "a value above the ceiling must be rejected")

	p.MaxProofSizeBytes = 0
	require.Error(t, p.Validate(), "zero is still rejected")
}
