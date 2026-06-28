// SPDX-License-Identifier: Apache-2.0

package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Compile-time assertion that the sdk.Msg (proto.Message) interface is implemented.
var (
	_ sdk.Msg = &MsgRegisterIdentity{}
	_ sdk.Msg = &MsgRevokeIdentity{}
	_ sdk.Msg = &MsgUpdateParams{}
	_ sdk.Msg = &MsgRotateIdentityKey{}
	_ sdk.Msg = &MsgRegisterTrustedIssuer{}
	_ sdk.Msg = &MsgRevokeTrustedIssuer{}
)

// MaxNonceLen bounds the attestation nonce length (a small anti-replay value).
const MaxNonceLen = 64

// ValidateBasic performs stateless validation of identity registration.
func (m *MsgRegisterIdentity) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Creator); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator: %s", err)
	}
	if err := ValidateDID(m.Did); err != nil {
		return errors.Wrap(ErrInvalidDID, err.Error())
	}
	if len(m.PubKey) == 0 || len(m.PubKey) > MaxPubKeyLen {
		return errors.Wrapf(ErrInvalidPubKey, "pub_key length %d (must be 1..%d)", len(m.PubKey), MaxPubKeyLen)
	}
	if len(m.UniquenessHash) == 0 || len(m.UniquenessHash) > MaxUniquenessHashLen {
		return errors.Wrapf(ErrInvalidUniqueness, "uniqueness_hash length %d (must be 1..%d)", len(m.UniquenessHash), MaxUniquenessHashLen)
	}
	if err := ValidateDID(m.IssuerDid); err != nil {
		return errors.Wrap(ErrInvalidDID, "issuer_did: "+err.Error())
	}
	if len(m.IssuerSig) == 0 || len(m.IssuerSig) > MaxIssuerSigLen {
		return errors.Wrapf(ErrInvalidIssuerSig, "issuer_sig length %d (must be 1..%d)", len(m.IssuerSig), MaxIssuerSigLen)
	}
	if len(m.Nonce) == 0 || len(m.Nonce) > MaxNonceLen {
		return errors.Wrapf(ErrInvalidIssuerSig, "nonce length %d (must be 1..%d)", len(m.Nonce), MaxNonceLen)
	}
	if len(m.PopSig) == 0 || len(m.PopSig) > MaxIssuerSigLen {
		return errors.Wrapf(ErrInvalidPoP, "pop_sig length %d (must be 1..%d)", len(m.PopSig), MaxIssuerSigLen)
	}
	return nil
}

// ValidateBasic performs stateless validation of a key rotation.
func (m *MsgRotateIdentityKey) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Creator); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator: %s", err)
	}
	if err := ValidateDID(m.Did); err != nil {
		return errors.Wrap(ErrInvalidDID, err.Error())
	}
	if len(m.NewPubKey) == 0 || len(m.NewPubKey) > MaxPubKeyLen {
		return errors.Wrapf(ErrInvalidPubKey, "new_pub_key length %d (must be 1..%d)", len(m.NewPubKey), MaxPubKeyLen)
	}
	if len(m.PopSig) == 0 || len(m.PopSig) > MaxIssuerSigLen {
		return errors.Wrapf(ErrInvalidPoP, "pop_sig length %d (must be 1..%d)", len(m.PopSig), MaxIssuerSigLen)
	}
	return nil
}

// ValidateBasic performs stateless validation of a trusted-issuer registration.
func (m *MsgRegisterTrustedIssuer) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid authority: %s", err)
	}
	if err := ValidateDID(m.Issuer.Did); err != nil {
		return errors.Wrap(ErrInvalidDID, "issuer.did: "+err.Error())
	}
	if len(m.Issuer.PubKey) == 0 || len(m.Issuer.PubKey) > MaxPubKeyLen {
		return errors.Wrapf(ErrInvalidPubKey, "issuer.pub_key length %d (must be 1..%d)", len(m.Issuer.PubKey), MaxPubKeyLen)
	}
	return nil
}

// ValidateBasic performs stateless validation of a trusted-issuer revocation.
func (m *MsgRevokeTrustedIssuer) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid authority: %s", err)
	}
	if err := ValidateDID(m.Did); err != nil {
		return errors.Wrap(ErrInvalidDID, err.Error())
	}
	return nil
}

// ValidateBasic performs stateless validation of identity revocation.
func (m *MsgRevokeIdentity) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Creator); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator: %s", err)
	}
	if m.Did == "" {
		return errors.Wrap(ErrInvalidDID, "did cannot be empty")
	}
	return nil
}

// ValidateBasic performs stateless validation of the params update.
func (m *MsgUpdateParams) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid authority: %s", err)
	}
	return m.Params.Validate()
}
