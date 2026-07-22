// SPDX-License-Identifier: Apache-2.0

package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Compile-time assertions that the messages implement sdk.Msg.
var (
	_ sdk.Msg = &MsgRegisterCredentialTemplate{}
	_ sdk.Msg = &MsgUpdateCredentialTemplate{}
	_ sdk.Msg = &MsgDeprecateCredentialTemplate{}
	_ sdk.Msg = &MsgAnchorCredential{}
	_ sdk.Msg = &MsgRevokeCredential{}
	_ sdk.Msg = &MsgCreateAgreement{}
	_ sdk.Msg = &MsgSignAgreement{}
	_ sdk.Msg = &MsgCancelAgreement{}
	_ sdk.Msg = &MsgAnchorPersonal{}
	_ sdk.Msg = &MsgUpdateParams{}
)

// ValidateBasic checks stateless validity of a template registration.
func (m *MsgRegisterCredentialTemplate) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Creator); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator: %s", err)
	}
	if m.Id == "" {
		return errors.Wrap(ErrInvalidRequest, "id cannot be empty")
	}
	if m.OwnerDid == "" {
		return errors.Wrap(ErrInvalidRequest, "owner_did cannot be empty")
	}
	if len(m.SchemaHash) == 0 {
		return errors.Wrap(ErrInvalidRequest, "schema_hash cannot be empty")
	}
	return nil
}

// ValidateBasic checks stateless validity of a template update.
func (m *MsgUpdateCredentialTemplate) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Creator); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator: %s", err)
	}
	if m.Id == "" {
		return errors.Wrap(ErrInvalidRequest, "id cannot be empty")
	}
	if len(m.SchemaHash) == 0 {
		return errors.Wrap(ErrInvalidRequest, "schema_hash cannot be empty")
	}
	return nil
}

// ValidateBasic checks stateless validity of a template deprecation.
func (m *MsgDeprecateCredentialTemplate) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Creator); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator: %s", err)
	}
	if m.Id == "" {
		return errors.Wrap(ErrInvalidRequest, "id cannot be empty")
	}
	return nil
}

// ValidateBasic checks stateless validity of a credential anchor.
func (m *MsgAnchorCredential) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Issuer); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid issuer: %s", err)
	}
	if m.IssuerDid == "" {
		return errors.Wrap(ErrInvalidRequest, "issuer_did cannot be empty")
	}
	if m.SubjectDid == "" {
		return errors.Wrap(ErrInvalidRequest, "subject_did cannot be empty")
	}
	if m.TemplateId == "" {
		return errors.Wrap(ErrInvalidRequest, "template_id cannot be empty")
	}
	if len(m.CredentialHash) == 0 {
		return errors.Wrap(ErrInvalidRequest, "credential_hash cannot be empty")
	}
	if len(m.IssuerSig) == 0 {
		return errors.Wrap(ErrInvalidRequest, "issuer_sig cannot be empty")
	}
	return nil
}

// ValidateBasic checks stateless validity of a credential revocation.
func (m *MsgRevokeCredential) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Issuer); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid issuer: %s", err)
	}
	if len(m.CredentialHash) == 0 {
		return errors.Wrap(ErrInvalidRequest, "credential_hash cannot be empty")
	}
	return nil
}

// ValidateBasic checks stateless validity of an agreement creation.
func (m *MsgCreateAgreement) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Creator); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator: %s", err)
	}
	if len(m.Hash) == 0 {
		return errors.Wrap(ErrInvalidRequest, "hash cannot be empty")
	}
	if len(m.RequiredSigners) == 0 {
		return errors.Wrap(ErrInvalidRequest, "required_signers cannot be empty")
	}
	seen := make(map[string]bool, len(m.RequiredSigners))
	for _, did := range m.RequiredSigners {
		if did == "" {
			return errors.Wrap(ErrInvalidRequest, "required_signers contains an empty DID")
		}
		if seen[did] {
			return errors.Wrapf(ErrInvalidRequest, "duplicate required signer: %s", did)
		}
		seen[did] = true
	}
	if m.Deadline < 0 {
		return errors.Wrap(ErrInvalidRequest, "deadline cannot be negative")
	}
	return nil
}

// ValidateBasic checks stateless validity of an agreement signature.
func (m *MsgSignAgreement) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Signer); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid signer: %s", err)
	}
	if len(m.Hash) == 0 {
		return errors.Wrap(ErrInvalidRequest, "hash cannot be empty")
	}
	if m.SignerDid == "" {
		return errors.Wrap(ErrInvalidRequest, "signer_did cannot be empty")
	}
	if len(m.Signature) == 0 {
		return errors.Wrap(ErrInvalidRequest, "signature cannot be empty")
	}
	return nil
}

// ValidateBasic checks stateless validity of an agreement cancellation.
func (m *MsgCancelAgreement) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Creator); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator: %s", err)
	}
	if len(m.Hash) == 0 {
		return errors.Wrap(ErrInvalidRequest, "hash cannot be empty")
	}
	return nil
}

// ValidateBasic checks stateless validity of a personal anchor.
func (m *MsgAnchorPersonal) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Owner); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid owner: %s", err)
	}
	if m.OwnerDid == "" {
		return errors.Wrap(ErrInvalidRequest, "owner_did cannot be empty")
	}
	if len(m.AnchorHash) == 0 {
		return errors.Wrap(ErrInvalidRequest, "anchor_hash cannot be empty")
	}
	if len(m.Signature) == 0 {
		return errors.Wrap(ErrInvalidRequest, "signature cannot be empty")
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
