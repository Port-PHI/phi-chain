// SPDX-License-Identifier: Apache-2.0

package types

import "cosmossdk.io/errors"

// institutions module errors (codes start at 2).
var (
	ErrInstitutionExists      = errors.Register(ModuleName, 2, "institution already exists")
	ErrInstitutionNotFound    = errors.Register(ModuleName, 3, "institution not found")
	ErrInstitutionFrozen      = errors.Register(ModuleName, 4, "institution is frozen — minting halted")
	ErrInvalidAmount          = errors.Register(ModuleName, 5, "invalid amount")
	ErrMintExceedsBacking     = errors.Register(ModuleName, 6, "mint exceeds attested backing of the institution")
	ErrInsufficientVault      = errors.Register(ModuleName, 7, "redeem exceeds institution vault balance")
	ErrUnauthorized           = errors.Register(ModuleName, 8, "unauthorized")
	ErrMintPaused             = errors.Register(ModuleName, 9, "minting is paused for this institution")
	ErrVaultNotEmpty          = errors.Register(ModuleName, 10, "institution vault is not empty — wind-down required before removal")
	ErrInvalidParams          = errors.Register(ModuleName, 11, "invalid params")
	ErrNonIntegralMint        = errors.Register(ModuleName, 12, "amount does not convert to an integral uphi value at the current rate")
	ErrSolvencyBroken         = errors.Register(ModuleName, 13, "operation would break the global solvency invariant")
	ErrRoleNotAuthorized      = errors.Register(ModuleName, 14, "signer role is not authorized for this institution action")
	ErrInvalidRole            = errors.Register(ModuleName, 15, "invalid institution role")
	ErrCapExceeded            = errors.Register(ModuleName, 16, "operation exceeds an institution cap (per-tx/daily/per-user)")
	ErrLooserThanFloor        = errors.Register(ModuleName, 17, "redeem cap may not be set below the protocol floor (user protection)")
	ErrDuplicateDeposit       = errors.Register(ModuleName, 18, "duplicate deposit/redeem reference — already processed (idempotency)")
	ErrIDTooLong              = errors.Register(ModuleName, 19, "institution id exceeds maximum length")
	ErrInvalidFxMetadata      = errors.Register(ModuleName, 20, "invalid fx provenance metadata for the institution type")
	ErrInvalidInstitutionType = errors.Register(ModuleName, 21, "invalid institution type — must be financial or fx")
	ErrInvalidDepositProof    = errors.Register(ModuleName, 22, "invalid or missing deposit proof for mint")
	ErrKycTierExceeded        = errors.Register(ModuleName, 23, "operation exceeds the holder's KYC tier limit")
	ErrRedemptionThrottled    = errors.Register(ModuleName, 24, "redemption exceeds the emergency stepped limit (30/60/90)")
	ErrFxOnboarding           = errors.Register(ModuleName, 25, "invalid fx onboarding request state")
	ErrGuarantorRequired      = errors.Register(ModuleName, 26, "fx institution requires an active financial guarantor")
	ErrMissingRef             = errors.Register(ModuleName, 27, "deposit/redeem reference must not be empty")
	ErrNothingRedeemed        = errors.Register(ModuleName, 28, "the carve-out consumes the entire redemption")
	ErrAttestationStale       = errors.Register(ModuleName, 29, "reserve attestation is stale: minting is closed")
	ErrTooFewAdmins           = errors.Register(ModuleName, 30, "minting requires at least two distinct admin keys")
	ErrAttestorIsMinter       = errors.Register(ModuleName, 31, "the reserve attestor may not authorise the mint against it")
	ErrBelowMinRedeem         = errors.Register(ModuleName, 32, "redemption is below the minimum redeemable amount")
	ErrRemovalInProgress      = errors.Register(ModuleName, 33, "institution removal is draining — the id cannot be re-registered until the purge completes")
)
