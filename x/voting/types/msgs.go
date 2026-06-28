// SPDX-License-Identifier: Apache-2.0

package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Compile-time assertions that the messages implement sdk.Msg.
var (
	_ sdk.Msg = &MsgCreateElection{}
	_ sdk.Msg = &MsgCastVote{}
	_ sdk.Msg = &MsgCloseElection{}
	_ sdk.Msg = &MsgCancelElection{}
	_ sdk.Msg = &MsgUpdateParams{}
)

// ValidateBasic checks stateless validity of an election creation.
func (m *MsgCreateElection) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Creator); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator: %s", err)
	}
	if m.Id == "" {
		return errors.Wrap(ErrInvalidRequest, "id cannot be empty")
	}
	if len(m.Id) > MaxElectionIDLen {
		return errors.Wrapf(ErrInvalidRequest, "id exceeds %d bytes", MaxElectionIDLen)
	}
	if m.Title == "" {
		return errors.Wrap(ErrInvalidRequest, "title cannot be empty")
	}
	if len(m.Title) > MaxTitleLen {
		return errors.Wrapf(ErrInvalidRequest, "title exceeds %d bytes", MaxTitleLen)
	}
	if len(m.Options) < 2 {
		return errors.Wrap(ErrInvalidRequest, "at least two options are required")
	}
	for i, o := range m.Options {
		if o == "" {
			return errors.Wrapf(ErrInvalidRequest, "option %d is empty", i)
		}
		if len(o) > MaxOptionLen {
			return errors.Wrapf(ErrInvalidRequest, "option %d exceeds %d bytes", i, MaxOptionLen)
		}
	}
	if m.RequiredTemplateId == "" {
		return errors.Wrap(ErrInvalidRequest, "required_template_id cannot be empty")
	}
	if m.VotingEnd <= m.VotingStart {
		return errors.Wrap(ErrInvalidRequest, "voting_end must be after voting_start")
	}
	return nil
}

// ValidateBasic checks stateless validity of a cast vote.
func (m *MsgCastVote) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Voter); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid voter: %s", err)
	}
	if m.ElectionId == "" {
		return errors.Wrap(ErrInvalidRequest, "election_id cannot be empty")
	}
	if len(m.Nullifier) == 0 {
		return errors.Wrap(ErrInvalidRequest, "nullifier cannot be empty")
	}
	if len(m.Nullifier) > MaxNullifierLen {
		return errors.Wrapf(ErrInvalidRequest, "nullifier exceeds %d bytes", MaxNullifierLen)
	}
	if len(m.EligibilityProof) == 0 {
		return errors.Wrap(ErrInvalidRequest, "eligibility_proof cannot be empty")
	}
	return nil
}

// ValidateBasic checks stateless validity of an election close.
func (m *MsgCloseElection) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Creator); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator: %s", err)
	}
	if m.ElectionId == "" {
		return errors.Wrap(ErrInvalidRequest, "election_id cannot be empty")
	}
	return nil
}

// ValidateBasic checks stateless validity of an election cancellation.
func (m *MsgCancelElection) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Creator); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator: %s", err)
	}
	if m.ElectionId == "" {
		return errors.Wrap(ErrInvalidRequest, "election_id cannot be empty")
	}
	return nil
}

// ValidateBasic checks stateless validity of a params update.
func (m *MsgUpdateParams) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid authority: %s", err)
	}
	return m.Params.Validate()
}
