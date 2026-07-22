// SPDX-License-Identifier: Apache-2.0

package types

import "cosmossdk.io/errors"

// Errors for the identity module (code 1 is reserved for internal errors; numbering starts at 2).
var (
	ErrIdentityExists           = errors.Register(ModuleName, 2, "identity already exists")
	ErrUniquenessUsed           = errors.Register(ModuleName, 3, "uniqueness marker already used")
	ErrIdentityNotFound         = errors.Register(ModuleName, 4, "identity not found")
	ErrInvalidDID               = errors.Register(ModuleName, 5, "invalid DID")
	ErrInvalidPubKey            = errors.Register(ModuleName, 6, "invalid public key")
	ErrInvalidUniqueness        = errors.Register(ModuleName, 7, "invalid uniqueness marker")
	ErrUnauthorized             = errors.Register(ModuleName, 8, "unauthorized")
	ErrInvalidParams            = errors.Register(ModuleName, 9, "invalid params")
	ErrIdentityRevoked          = errors.Register(ModuleName, 10, "identity already revoked")
	ErrInvalidIssuerSig         = errors.Register(ModuleName, 14, "invalid or missing issuer signature")
	ErrIssuerNotTrusted         = errors.Register(ModuleName, 15, "issuer is not a trusted, active identity issuer")
	ErrInvalidPoP               = errors.Register(ModuleName, 16, "invalid or missing proof-of-possession signature")
	ErrIssuerNotFound           = errors.Register(ModuleName, 17, "trusted issuer not found")
	ErrKeyRotation              = errors.Register(ModuleName, 18, "invalid key rotation")
	ErrNonceReused              = errors.Register(ModuleName, 19, "issuer attestation nonce already used (replay)")
	ErrInvalidStatusTransition  = errors.Register(ModuleName, 20, "invalid identity status transition")
	ErrInvalidGuardians         = errors.Register(ModuleName, 21, "invalid guardian set")
	ErrInvalidRecovery          = errors.Register(ModuleName, 22, "invalid recovery request")
	ErrRecoveryNotFound         = errors.Register(ModuleName, 23, "recovery request not found")
	ErrRecoveryNotPending       = errors.Register(ModuleName, 24, "recovery request is not pending")
	ErrRecoveryTooEarly         = errors.Register(ModuleName, 25, "recovery opposition window has not elapsed")
	ErrRecoveryBelowQuorum      = errors.Register(ModuleName, 26, "recovery approvals are below the guardian threshold")
	ErrRecoverySlotsFull        = errors.Register(ModuleName, 27, "too many open recovery requests for this DID")
	ErrRecoveryNonceReused      = errors.Register(ModuleName, 28, "recovery nonce already used (replay)")
	ErrNotAGuardian             = errors.Register(ModuleName, 29, "the revealed guardian is not in this DID's guardian set")
	ErrGuardianNotEligible      = errors.Register(ModuleName, 30, "guardian is not an existing ACTIVE identity")
	ErrAlreadyApproved          = errors.Register(ModuleName, 31, "this guardian has already approved")
	ErrRecoveryKeyCollision     = errors.Register(ModuleName, 32, "the proposed key already self-certifies a registered DID")
	ErrReauthNotEnabled         = errors.Register(ModuleName, 33, "re-authentication recovery is not enabled in this build")
	ErrInvalidReauthAttestation = errors.Register(ModuleName, 34, "invalid or missing re-authentication attestation")
	ErrAlreadyRejected          = errors.Register(ModuleName, 35, "this guardian has already rejected")
	ErrValidatorNeedsDID        = errors.Register(ModuleName, 11, "validator account must have an active DID (verified unique human)")
	ErrDIDAlreadyValidator      = errors.Register(ModuleName, 12, "DID already backs another validator (one unique human per validator)")
	ErrMinSelfDelegation        = errors.Register(ModuleName, 13, "validator min_self_delegation is below the protocol floor")
)
