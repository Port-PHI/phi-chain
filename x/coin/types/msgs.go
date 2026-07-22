// SPDX-License-Identifier: Apache-2.0

package types

import (
	"cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	_ sdk.Msg = &MsgTransfer{}
	_ sdk.Msg = &MsgUpdateParams{}
	_ sdk.Msg = &MsgWithdrawRevenue{}
)

// ValidateBasic performs stateless validation of a transfer.
func (m *MsgTransfer) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.From); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid from: %s", err)
	}
	if _, err := sdk.AccAddressFromBech32(m.To); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid to: %s", err)
	}
	if m.From == m.To {
		return ErrSameAccount
	}
	amt, ok := math.NewIntFromString(m.Amount)
	if !ok || !amt.IsPositive() {
		return errors.Wrapf(ErrInvalidAmount, "amount: %q", m.Amount)
	}
	return nil
}

// ValidateBasic performs stateless validation of a params update.
func (m *MsgUpdateParams) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid authority: %s", err)
	}
	return m.Params.Validate()
}

// ValidateBasic performs stateless validation of a revenue withdrawal.
func (m *MsgWithdrawRevenue) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid authority: %s", err)
	}
	amt, ok := math.NewIntFromString(m.Amount)
	if !ok || !amt.IsPositive() {
		return errors.Wrapf(ErrInvalidAmount, "amount: %q", m.Amount)
	}
	return nil
}
