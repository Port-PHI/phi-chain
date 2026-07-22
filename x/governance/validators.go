// SPDX-License-Identifier: Apache-2.0

package governance

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// ValidatorSource abstracts the staking keeper for the TECHNICAL route's vote eligibility.
type ValidatorSource interface {
	// IsActiveValidatorOperator reports whether addr is the operator account of a BONDED validator.
	IsActiveValidatorOperator(ctx sdk.Context, addr []byte) bool
}

// StakingKeeper is the slice of the staking keeper the validator source needs.
type StakingKeeper interface {
	GetValidator(ctx context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error)
}

// StakingValidatorSource is the production ValidatorSource, reading the live staking registry.
type StakingValidatorSource struct {
	sk StakingKeeper
}

// NewStakingValidatorSource adapts a staking keeper to ValidatorSource.
func NewStakingValidatorSource(sk StakingKeeper) StakingValidatorSource {
	return StakingValidatorSource{sk: sk}
}

// IsActiveValidatorOperator reports whether addr belongs to a bonded validator.
func (s StakingValidatorSource) IsActiveValidatorOperator(ctx sdk.Context, addr []byte) bool {
	v, err := s.sk.GetValidator(ctx, sdk.ValAddress(addr))
	if err != nil {
		return false
	}
	return v.IsBonded()
}
