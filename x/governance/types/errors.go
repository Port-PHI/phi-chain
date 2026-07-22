// SPDX-License-Identifier: Apache-2.0

package types

import "cosmossdk.io/errors"

var (
	// ErrNotEligibleToVote: ballot cast by a controller ineligible under the proposal's frozen basis.
	ErrNotEligibleToVote = errors.Register(ModuleName, 2, "voter is not an eligible controller for this proposal")
)
