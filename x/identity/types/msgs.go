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
	_ sdk.Msg = &MsgUpdateStatus{}
	_ sdk.Msg = &MsgSetGuardians{}
	_ sdk.Msg = &MsgInitiateRecovery{}
	_ sdk.Msg = &MsgApproveRecovery{}
	_ sdk.Msg = &MsgRejectRecovery{}
	_ sdk.Msg = &MsgExecuteRecovery{}
	_ sdk.Msg = &MsgCancelRecovery{}
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
	// The registrant's curve: r1 (device passkey, the default and the zero value) or k1 (the opt-in self-custody path).
	if _, err := CurveForKeyType(m.KeyType); err != nil {
		return errors.Wrap(ErrInvalidPubKey, err.Error())
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

// ValidateBasic performs stateless validation of an identity status update.
func (m *MsgUpdateStatus) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid authority: %s", err)
	}
	if err := ValidateDID(m.Did); err != nil {
		return errors.Wrap(ErrInvalidDID, err.Error())
	}
	if m.NewStatus != DID_STATUS_ACTIVE && m.NewStatus != DID_STATUS_SUSPENDED {
		return errors.Wrapf(ErrInvalidStatusTransition, "new_status must be ACTIVE or SUSPENDED, got %s", m.NewStatus)
	}
	return nil
}

// ValidateBasic performs stateless validation of a guardian-commitment-set replacement.
func (m *MsgSetGuardians) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Controller); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid controller: %s", err)
	}
	return ValidateGuardianSetBasic(m.Did, m.Commitments, m.Threshold)
}

// ValidateBasic performs stateless validation of a recovery initiation.
func (m *MsgInitiateRecovery) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Creator); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator: %s", err)
	}
	if err := ValidateDID(m.Did); err != nil {
		return errors.Wrap(ErrInvalidDID, err.Error())
	}
	if len(m.ProposedNewPubKey) == 0 || len(m.ProposedNewPubKey) > MaxPubKeyLen {
		return errors.Wrapf(ErrInvalidPubKey, "proposed_new_pub_key length %d (must be 1..%d)",
			len(m.ProposedNewPubKey), MaxPubKeyLen)
	}
	// Recovery rotates onto a P-256 passkey.
	if m.KeyType != KEY_TYPE_SECP256R1 {
		return errors.Wrapf(ErrInvalidRecovery, "key_type must be SECP256R1, got %s", m.KeyType)
	}
	if err := ValidateRecoveryNonce(m.Nonce); err != nil {
		return err
	}
	// Proof-of-possession of the new key is required by BOTH methods — REAUTH included, so that a compromised attestor cannot on its own install a key nobody holds.
	if len(m.PopSig) == 0 || len(m.PopSig) > MaxRecoverySigLen {
		return errors.Wrapf(ErrInvalidPoP, "pop_sig length %d (must be 1..%d)", len(m.PopSig), MaxRecoverySigLen)
	}
	// Each method carries exactly its own authorisation material — never the other's.
	switch m.Method {
	case RECOVERY_METHOD_SOCIAL:
		if len(m.ReauthAttestation) != 0 || m.AttestorDid != "" {
			return errors.Wrap(ErrInvalidRecovery, "SOCIAL recovery must not carry a re-auth attestation")
		}
	case RECOVERY_METHOD_REAUTH:
		if len(m.ReauthAttestation) == 0 || len(m.ReauthAttestation) > MaxRecoverySigLen {
			return errors.Wrapf(ErrInvalidReauthAttestation, "reauth_attestation length %d (must be 1..%d)",
				len(m.ReauthAttestation), MaxRecoverySigLen)
		}
		if err := ValidateDID(m.AttestorDid); err != nil {
			return errors.Wrap(ErrInvalidDID, "attestor_did: "+err.Error())
		}
	default:
		return errors.Wrapf(ErrInvalidRecovery, "method must be SOCIAL or REAUTH, got %s", m.Method)
	}
	return nil
}

// ValidateBasic performs stateless validation of a guardian approval.
func (m *MsgApproveRecovery) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Creator); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator: %s", err)
	}
	if len(m.RecoveryId) != RecoveryIDLen {
		return errors.Wrapf(ErrInvalidRecovery, "recovery_id length %d (must be %d)", len(m.RecoveryId), RecoveryIDLen)
	}
	if err := ValidateDID(m.GuardianDid); err != nil {
		return errors.Wrap(ErrInvalidDID, "guardian_did: "+err.Error())
	}
	if len(m.Salt) != GuardianSaltLen {
		return errors.Wrapf(ErrInvalidGuardians, "salt length %d (must be %d)", len(m.Salt), GuardianSaltLen)
	}
	return nil
}

// ValidateBasic performs stateless validation of a guardian's rejection.
func (m *MsgRejectRecovery) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Creator); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator: %s", err)
	}
	if len(m.RecoveryId) != RecoveryIDLen {
		return errors.Wrapf(ErrInvalidRecovery, "recovery_id length %d (must be %d)", len(m.RecoveryId), RecoveryIDLen)
	}
	if err := ValidateDID(m.GuardianDid); err != nil {
		return errors.Wrap(ErrInvalidDID, "guardian_did: "+err.Error())
	}
	if len(m.Salt) != GuardianSaltLen {
		return errors.Wrapf(ErrInvalidGuardians, "salt length %d (must be %d)", len(m.Salt), GuardianSaltLen)
	}
	return nil
}

// ValidateBasic performs stateless validation of a recovery execution (permissionless crank).
func (m *MsgExecuteRecovery) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Creator); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator: %s", err)
	}
	if len(m.RecoveryId) != RecoveryIDLen {
		return errors.Wrapf(ErrInvalidRecovery, "recovery_id length %d (must be %d)", len(m.RecoveryId), RecoveryIDLen)
	}
	return nil
}

// ValidateBasic performs stateless validation of a recovery cancellation.
func (m *MsgCancelRecovery) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Creator); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator: %s", err)
	}
	if len(m.RecoveryId) != RecoveryIDLen {
		return errors.Wrapf(ErrInvalidRecovery, "recovery_id length %d (must be %d)", len(m.RecoveryId), RecoveryIDLen)
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
