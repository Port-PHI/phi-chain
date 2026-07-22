//go:build voting_snark

// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"cosmossdk.io/errors"

	"github.com/Port-PHI/phi-chain/x/voting/types"
)

func checkNullifierShape(nullifier []byte) error {
	if len(nullifier) != types.NullifierPointLen {
		return errors.Wrapf(types.ErrInvalidNullifier, "nullifier must be %d bytes, got %d", types.NullifierPointLen, len(nullifier))
	}
	return nil
}
