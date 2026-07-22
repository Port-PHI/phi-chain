// SPDX-License-Identifier: Apache-2.0

package types

import (
	"encoding/hex"

	"cosmossdk.io/errors"
)

// ValidateGuardianSetBasic performs the stateless, param-independent guardian-set checks shared by MsgSetGuardians.ValidateBasic and genesis validation: a well-formed protected DID, at least one guardian commitment, commitments of exactly GuardianCommitmentLen bytes, DISTINCT commitments, and 1 <= threshold <= len(commitments).
func ValidateGuardianSetBasic(did string, commitments [][]byte, threshold uint32) error {
	if err := ValidateDID(did); err != nil {
		return errors.Wrap(ErrInvalidDID, err.Error())
	}
	if len(commitments) == 0 {
		return errors.Wrap(ErrInvalidGuardians, "at least one guardian commitment is required")
	}
	seen := make(map[string]struct{}, len(commitments))
	for i, c := range commitments {
		if len(c) != GuardianCommitmentLen {
			return errors.Wrapf(ErrInvalidGuardians,
				"commitments[%d]: length %d (must be exactly %d)", i, len(c), GuardianCommitmentLen)
		}
		key := hex.EncodeToString(c)
		if _, dup := seen[key]; dup {
			return errors.Wrapf(ErrInvalidGuardians, "duplicate guardian commitment at index %d", i)
		}
		seen[key] = struct{}{}
	}
	if threshold == 0 {
		return errors.Wrap(ErrInvalidGuardians, "threshold must be >= 1")
	}
	if uint64(threshold) > uint64(len(commitments)) {
		return errors.Wrapf(ErrInvalidGuardians,
			"threshold %d exceeds guardian count %d", threshold, len(commitments))
	}
	return nil
}

// HasCommitment reports whether c is one of the set's commitments.
func (gs GuardianSet) HasCommitment(c []byte) bool {
	for _, have := range gs.Commitments {
		if len(have) == len(c) && subtleEqual(have, c) {
			return true
		}
	}
	return false
}

func subtleEqual(a, b []byte) bool {
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
