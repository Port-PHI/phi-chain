// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ValidateGenesisFounders: every bonded, unjailed genesis validator must hold an ACTIVE DID, else the first binding sweep would remove the whole validator set.
func (k Keeper) ValidateGenesisFounders(ctx sdk.Context, sk ValidatorGenesisStaking) error {
	validators, err := sk.GetAllValidators(ctx)
	if err != nil {
		return fmt.Errorf("identity genesis: cannot read the validator set: %w", err)
	}

	var missing []string
	for _, v := range validators {
		if v.IsJailed() || !v.IsBonded() {
			continue
		}
		valAddr, err := sdk.ValAddressFromBech32(v.GetOperator())
		if err != nil {
			return fmt.Errorf("identity genesis: validator %q has an undecodable operator address: %w",
				v.GetOperator(), err)
		}
		if _, ok := k.PrimaryDID(ctx, sdk.AccAddress(valAddr).String()); !ok {
			missing = append(missing, v.GetOperator())
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf(
			"identity genesis: %d active validator(s) have no ACTIVE DID and would be removed by the "+
				"first binding sweep: %v — every genesis validator's operator account must hold an "+
				"ACTIVE identity in the identity genesis",
			len(missing), missing)
	}
	return nil
}
