// SPDX-License-Identifier: Apache-2.0

package types

// Event keys for the institutions module.
const (
	EventTypeInstitutionRegistered = "institution_registered"
	EventTypeInstitutionRemoved    = "institution_removed"
	EventTypeInstitutionMinted     = "institution_minted"
	EventTypeInstitutionRedeemed   = "institution_redeemed"
	EventTypeAttestationPublished  = "attestation_published"
	EventTypeInstitutionFrozen     = "institution_frozen"
	// backing health: emitted when an institution's vault exceeds its attested reserve. This is
	// an allowed (LOW_LIQ) state, not a consensus halt — a monitoring signal for phi-bridge.
	EventTypeBackingShortfall = "institution_backing_shortfall"
	// RBAC and sensitive actions.
	EventTypeRoleGranted   = "institution_role_granted"
	EventTypeRoleRevoked   = "institution_role_revoked"
	EventTypeAppConfigSet  = "institution_app_config_set"
	EventTypeParamsSet     = "institution_params_set"
	EventTypeActionPending = "institution_action_pending" // sensitive action awaiting more signatures
	EventTypeDepositKeySet = "institution_deposit_key_set"
	// emergency stepped redemption.
	EventTypeEmergencyRedemption = "emergency_redemption_set"
	// penalty escrow: slashed-stake compensation was minted (supply restored) but could not be routed
	// to the penalty destination, so it is held in the module account for governance to sweep.
	EventTypePenaltyEscrowed = "institution_penalty_escrowed"
	// fx onboarding (guarantor + public vote).
	EventTypeFxEntryRequested  = "fx_entry_requested"
	EventTypeFxEntryGuaranteed = "fx_entry_guaranteed"
	EventTypeFxEntryDeclined   = "fx_entry_declined"
	EventTypeFxEntryFinalized  = "fx_entry_finalized"

	AttributeKeyInstitution     = "institution"
	AttributeKeyGrantee         = "grantee"
	AttributeKeyRole            = "role"
	AttributeKeyAction          = "action"
	AttributeKeyApprovals       = "approvals"
	AttributeKeyThreshold       = "threshold"
	AttributeKeyDepositRef      = "deposit_ref"
	AttributeKeyAdmin           = "admin"
	AttributeKeyRecipient       = "recipient"
	AttributeKeyHolder          = "holder"
	AttributeKeyAmountToman     = "amount_toman"
	AttributeKeyMintedUphi      = "minted_uphi"
	AttributeKeyBurnedUphi      = "burned_uphi"
	AttributeKeyFeeToman        = "fee_toman"    // tiered coin-age exit penalty (toman)
	AttributeKeyPayoutToman     = "payout_toman" // net toman paid to the seller (phi-bridge)
	AttributeKeyAttestedReserve = "attested_reserve"
	AttributeKeyFrozen          = "frozen"
	AttributeKeyShortfallToman  = "shortfall_toman" // vault_balance − attested_reserve (health)
	// fx provenance (emitted on an fx institution's mint/redeem).
	AttributeKeyInstitutionType = "institution_type"
	AttributeKeyFxCurrency      = "fx_currency"
	AttributeKeyFxAmount        = "fx_amount"
	AttributeKeyFxTxRef         = "fx_tx_ref"
	// fx onboarding attributes.
	AttributeKeyFxID       = "fx_id"
	AttributeKeyApplicant  = "applicant"
	AttributeKeyGuarantor  = "guarantor_id"
	AttributeKeyProposalID = "proposal_id"
	// emergency redemption attributes.
	AttributeKeyActive    = "active"
	AttributeKeyStartedAt = "started_at"
	// penalty escrow attributes.
	AttributeKeyEscrowedUphi = "escrowed_uphi"
	AttributeKeyReason       = "reason"
)
