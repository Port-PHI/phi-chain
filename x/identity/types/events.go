// SPDX-License-Identifier: Apache-2.0

package types

// Event keys for the identity module.
const (
	EventTypeRegisterIdentity  = "register_identity"
	EventTypeRevokeIdentity    = "revoke_identity"
	EventTypeRotateIdentityKey = "rotate_identity_key"
	EventTypeStatusChanged     = "status_changed"
	EventTypeGuardiansSet      = "guardians_set"
	EventTypeBootstrapEnded    = "bootstrap_phase_ended"
	EventTypeRecoveryInitiated = "recovery_initiated"
	EventTypeRecoveryApproved  = "recovery_approved"
	EventTypeRecoveryRejected  = "recovery_rejected"
	EventTypeRecoveryExecuted  = "recovery_executed"
	EventTypeRecoveryCancelled = "recovery_cancelled"
	EventTypeRecoveryExpired   = "recovery_expired"
	// EventTypeRecoveryExtended records a request held open while the protected DID is suspended.
	EventTypeRecoveryExtended        = "recovery_extended"
	EventTypeRecoverySuperseded      = "recovery_superseded"
	EventTypeTrustedIssuerRegistered = "trusted_issuer_registered"
	EventTypeTrustedIssuerRevoked    = "trusted_issuer_revoked"
	EventTypeValidatorBindingSweep   = "validator_binding_sweep"
	// EventTypeValidatorSweepFailed: sweep could not act on a validator this block (non-fatal skip); surfaced as an event so a per-block failure reaches the indexer and monitoring.
	EventTypeValidatorSweepFailed = "validator_sweep_failed"

	AttributeKeyDID        = "did"
	AttributeKeyController = "controller"
	AttributeKeyIssuerDID  = "issuer_did"
	AttributeKeyCount      = "identity_count"
	AttributeKeyActive     = "active"
	AttributeKeyOldStatus  = "old_status"
	AttributeKeyNewStatus  = "new_status"
	AttributeKeyValidator  = "validator"
	AttributeKeyAction     = "action"
	AttributeKeyReason     = "reason"
	// guardian set (count only — no commitment or guardian identity is duplicated into the event stream).
	AttributeKeyGuardianCount = "guardian_count"
	AttributeKeyThreshold     = "threshold"
	AttributeKeyRecoveryID    = "recovery_id"
	AttributeKeyExecuteAfter  = "execute_after"
	AttributeKeyGuardianDID   = "guardian_did"
	AttributeKeyApprovals     = "approvals"
	AttributeKeyRejections    = "rejections"
	AttributeKeyDeposit       = "deposit_uphi"
	AttributeKeyNewController = "new_controller"
	AttributeKeyMethod        = "method"
	AttributeKeyAttestorDID   = "attestor_did"
	AttributeKeyFee           = "fee_uphi"
)
