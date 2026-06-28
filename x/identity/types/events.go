// SPDX-License-Identifier: Apache-2.0

package types

// Event keys for the identity module.
const (
	EventTypeRegisterIdentity  = "register_identity"
	EventTypeRevokeIdentity    = "revoke_identity"
	EventTypeRotateIdentityKey = "rotate_identity_key"
	EventTypeBootstrapEnded    = "bootstrap_phase_ended"
	// trusted issuer registry (gov).
	EventTypeTrustedIssuerRegistered = "trusted_issuer_registered"
	EventTypeTrustedIssuerRevoked    = "trusted_issuer_revoked"

	AttributeKeyDID        = "did"
	AttributeKeyController = "controller"
	AttributeKeyIssuerDID  = "issuer_did"
	AttributeKeyCount      = "identity_count"
	AttributeKeyActive     = "active"
)
