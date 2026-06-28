// SPDX-License-Identifier: Apache-2.0

package types

import (
	"cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	_ sdk.Msg = &MsgRegisterInstitution{}
	_ sdk.Msg = &MsgRemoveInstitution{}
	_ sdk.Msg = &MsgInstitutionMint{}
	_ sdk.Msg = &MsgInstitutionRedeem{}
	_ sdk.Msg = &MsgPublishInstitutionAttestation{}
	_ sdk.Msg = &MsgFreezeInstitution{}
	_ sdk.Msg = &MsgUpdateParams{}
	_ sdk.Msg = &MsgGrantInstitutionRole{}
	_ sdk.Msg = &MsgRevokeInstitutionRole{}
	_ sdk.Msg = &MsgUpdateInstitutionAppConfig{}
	_ sdk.Msg = &MsgUpdateInstitutionParams{}
	_ sdk.Msg = &MsgRequestFxEntry{}
	_ sdk.Msg = &MsgGuaranteeFxEntry{}
	_ sdk.Msg = &MsgFinalizeFxEntry{}
	_ sdk.Msg = &MsgSetInstitutionDepositKey{}
	_ sdk.Msg = &MsgSetEmergencyRedemption{}
)

func (m *MsgRegisterInstitution) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Operator); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid operator: %s", err)
	}
	if _, err := sdk.AccAddressFromBech32(m.Admin); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid admin: %s", err)
	}
	if m.Id == "" {
		return errors.Wrap(ErrInstitutionNotFound, "id cannot be empty")
	}
	if len(m.Id) > MaxInstitutionIDLen {
		return errors.Wrapf(ErrIDTooLong, "id length %d > %d", len(m.Id), MaxInstitutionIDLen)
	}
	if m.License == "" {
		return errors.Wrap(ErrInvalidParams, "license cannot be empty")
	}
	if m.InstitutionType != INSTITUTION_TYPE_FINANCIAL && m.InstitutionType != INSTITUTION_TYPE_FX {
		return errors.Wrapf(ErrInvalidInstitutionType, "institution_type=%d", m.InstitutionType)
	}
	if m.Bond != "" {
		if v, ok := math.NewIntFromString(m.Bond); !ok || v.IsNegative() {
			return errors.Wrapf(ErrInvalidAmount, "bond: %q", m.Bond)
		}
	}
	return nil
}

func (m *MsgRemoveInstitution) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Operator); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid operator: %s", err)
	}
	if m.Id == "" {
		return errors.Wrap(ErrInstitutionNotFound, "id cannot be empty")
	}
	return nil
}

func (m *MsgInstitutionMint) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Admin); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid admin: %s", err)
	}
	if _, err := sdk.AccAddressFromBech32(m.Recipient); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid recipient: %s", err)
	}
	if m.Institution == "" {
		return errors.Wrap(ErrInstitutionNotFound, "institution cannot be empty")
	}
	v, ok := math.NewIntFromString(m.AmountToman)
	if !ok || !v.IsPositive() {
		return errors.Wrapf(ErrInvalidAmount, "amount_toman: %q", m.AmountToman)
	}
	if err := validateRefLen("deposit_ref", m.DepositRef, MaxRefLen); err != nil {
		return err
	}
	return validateFxFieldLens(m.FxCurrency, m.FxAmount, m.FxTxRef)
}

func (m *MsgInstitutionRedeem) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Admin); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid admin: %s", err)
	}
	if _, err := sdk.AccAddressFromBech32(m.Holder); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid holder: %s", err)
	}
	// Holder consent: strict self-redeem — the signer (admin) must be the holder whose
	// uphi is burned, so an institution operator cannot force-burn a stranger's balance.
	if m.Admin != m.Holder {
		return errors.Wrap(ErrUnauthorized, "redeem must be signed by the holder (admin must equal holder)")
	}
	if m.Institution == "" {
		return errors.Wrap(ErrInstitutionNotFound, "institution cannot be empty")
	}
	v, ok := math.NewIntFromString(m.AmountToman)
	if !ok || !v.IsPositive() {
		return errors.Wrapf(ErrInvalidAmount, "amount_toman: %q", m.AmountToman)
	}
	if err := validateRefLen("redeem_ref", m.RedeemRef, MaxRefLen); err != nil {
		return err
	}
	return validateFxFieldLens(m.FxCurrency, m.FxAmount, m.FxTxRef)
}

// validateRefLen bounds a free-form reference written into a persistent KV key.
func validateRefLen(field, s string, max int) error {
	if len(s) > max {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest, "%s length %d exceeds %d", field, len(s), max)
	}
	return nil
}

// validateFxFieldLens bounds the fx provenance fields written into institution state/events.
func validateFxFieldLens(fxCurrency, fxAmount, fxTxRef string) error {
	for _, f := range []struct {
		name, val string
	}{{"fx_currency", fxCurrency}, {"fx_amount", fxAmount}, {"fx_tx_ref", fxTxRef}} {
		if len(f.val) > MaxFxFieldLen {
			return errors.Wrapf(sdkerrors.ErrInvalidRequest, "%s length %d exceeds %d", f.name, len(f.val), MaxFxFieldLen)
		}
	}
	return nil
}

func (m *MsgPublishInstitutionAttestation) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Admin); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid admin: %s", err)
	}
	if m.Institution == "" {
		return errors.Wrap(ErrInstitutionNotFound, "institution cannot be empty")
	}
	if v, ok := math.NewIntFromString(m.AttestedReserve); !ok || v.IsNegative() {
		return errors.Wrapf(ErrInvalidAmount, "attested_reserve: %q", m.AttestedReserve)
	}
	return nil
}

func (m *MsgFreezeInstitution) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Operator); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid operator: %s", err)
	}
	if m.Id == "" {
		return errors.Wrap(ErrInstitutionNotFound, "id cannot be empty")
	}
	return nil
}

func (m *MsgUpdateParams) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid authority: %s", err)
	}
	return m.Params.Validate()
}

// --- sensitive actions ---

func (m *MsgGrantInstitutionRole) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Signer); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid signer: %s", err)
	}
	if _, err := sdk.AccAddressFromBech32(m.Grantee); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid grantee: %s", err)
	}
	if m.Institution == "" {
		return errors.Wrap(ErrInstitutionNotFound, "institution cannot be empty")
	}
	// Valid role, other than UNSPECIFIED.
	if m.Role <= INSTITUTION_ROLE_UNSPECIFIED || m.Role > INSTITUTION_ROLE_VIEWER {
		return errors.Wrapf(ErrInvalidRole, "role %d", m.Role)
	}
	return nil
}

func (m *MsgRevokeInstitutionRole) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Signer); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid signer: %s", err)
	}
	if _, err := sdk.AccAddressFromBech32(m.Grantee); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid grantee: %s", err)
	}
	if m.Institution == "" {
		return errors.Wrap(ErrInstitutionNotFound, "institution cannot be empty")
	}
	return nil
}

func (m *MsgUpdateInstitutionAppConfig) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Signer); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid signer: %s", err)
	}
	if m.Institution == "" {
		return errors.Wrap(ErrInstitutionNotFound, "institution cannot be empty")
	}
	return nil
}

func (m *MsgUpdateInstitutionParams) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Signer); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid signer: %s", err)
	}
	if m.Institution == "" {
		return errors.Wrap(ErrInstitutionNotFound, "institution cannot be empty")
	}
	// Structural validity (non-negative caps); the "stricter only" rule is checked in the keeper.
	return m.Params.Validate()
}

// --- fx onboarding ---

func (m *MsgRequestFxEntry) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Applicant); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid applicant: %s", err)
	}
	if m.FxId == "" {
		return errors.Wrap(ErrFxOnboarding, "fx_id cannot be empty")
	}
	if len(m.FxId) > MaxInstitutionIDLen {
		return errors.Wrapf(ErrIDTooLong, "fx_id length %d > %d", len(m.FxId), MaxInstitutionIDLen)
	}
	// License must be present and bounded — mirrors MsgRegisterInstitution, so an fx
	// applicant cannot onboard with an empty or unbounded license string.
	if m.License == "" {
		return errors.Wrap(ErrInvalidParams, "license cannot be empty")
	}
	if len(m.License) > MaxLicenseLen {
		return errors.Wrapf(ErrInvalidParams, "license length %d > %d", len(m.License), MaxLicenseLen)
	}
	if m.GuarantorId == "" {
		return errors.Wrap(ErrGuarantorRequired, "guarantor_id cannot be empty")
	}
	return nil
}

func (m *MsgGuaranteeFxEntry) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.GuarantorAdmin); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid guarantor_admin: %s", err)
	}
	if m.FxId == "" {
		return errors.Wrap(ErrFxOnboarding, "fx_id cannot be empty")
	}
	return nil
}

func (m *MsgFinalizeFxEntry) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Operator); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid operator: %s", err)
	}
	if m.FxId == "" {
		return errors.Wrap(ErrFxOnboarding, "fx_id cannot be empty")
	}
	return nil
}

// --- deposit key / emergency redemption ---

func (m *MsgSetInstitutionDepositKey) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Signer); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid signer: %s", err)
	}
	if m.Institution == "" {
		return errors.Wrap(ErrInstitutionNotFound, "institution cannot be empty")
	}
	// A non-empty key must be a plausible P-256 SEC1 public key (33 compressed or 65 uncompressed bytes).
	if n := len(m.DepositPubkey); n != 0 && n != 33 && n != 65 {
		return errors.Wrapf(ErrInvalidDepositProof, "deposit_pubkey must be 33 or 65 bytes, got %d", n)
	}
	return nil
}

func (m *MsgSetEmergencyRedemption) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid authority: %s", err)
	}
	for _, v := range []string{m.CapBeforeDay30, m.CapFromDay30, m.CapFromDay60} {
		if v != "" {
			if n, ok := math.NewIntFromString(v); !ok || n.IsNegative() {
				return errors.Wrapf(ErrInvalidAmount, "emergency cap: %q", v)
			}
		}
	}
	return nil
}
