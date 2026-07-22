// SPDX-License-Identifier: Apache-2.0

package types

// Event types and attribute keys for x/credentials.
const (
	EventTypeRegisterTemplate  = "register_credential_template"
	EventTypeUpdateTemplate    = "update_credential_template"
	EventTypeDeprecateTemplate = "deprecate_credential_template"
	EventTypeAnchorCredential  = "anchor_credential"
	EventTypeRevokeCredential  = "revoke_credential"
	EventTypeCreateAgreement   = "create_agreement"
	EventTypeSignAgreement     = "sign_agreement"
	EventTypeCompleteAgreement = "complete_agreement"
	EventTypeCancelAgreement   = "cancel_agreement"
	EventTypeAnchorPersonal    = "anchor_personal"

	AttributeKeyTemplateID     = "template_id"
	AttributeKeyVersion        = "version"
	AttributeKeyOwnerDID       = "owner_did"
	AttributeKeyIssuerDID      = "issuer_did"
	AttributeKeySubjectDID     = "subject_did"
	AttributeKeyCredentialHash = "credential_hash"
	AttributeKeyAgreementHash  = "agreement_hash"
	AttributeKeySignerDID      = "signer_did"
	AttributeKeyAnchorHash     = "anchor_hash"
	AttributeKeyCompleted      = "completed"
)
