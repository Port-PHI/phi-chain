// SPDX-License-Identifier: Apache-2.0

package types

import "cosmossdk.io/errors"

// x/credentials errors (code 1 is reserved for internal errors; start at 2).
var (
	ErrTemplateExists          = errors.Register(ModuleName, 2, "credential template already exists")
	ErrTemplateNotFound        = errors.Register(ModuleName, 3, "credential template not found")
	ErrTemplateDeprecated      = errors.Register(ModuleName, 4, "credential template is deprecated")
	ErrTemplateVersionMismatch = errors.Register(ModuleName, 5, "credential template version mismatch")
	ErrCredentialExists        = errors.Register(ModuleName, 6, "credential already anchored")
	ErrCredentialNotFound      = errors.Register(ModuleName, 7, "credential anchor not found")
	ErrCredentialRevoked       = errors.Register(ModuleName, 8, "credential already revoked")
	ErrAgreementExists         = errors.Register(ModuleName, 9, "agreement already exists")
	ErrAgreementNotFound       = errors.Register(ModuleName, 10, "agreement not found")
	ErrAgreementClosed         = errors.Register(ModuleName, 11, "agreement is not open for this action")
	ErrAgreementExpired        = errors.Register(ModuleName, 12, "agreement deadline has passed")
	ErrNotRequiredSigner       = errors.Register(ModuleName, 13, "signer DID is not a required signer of this agreement")
	ErrAlreadySigned           = errors.Register(ModuleName, 14, "signer DID has already signed this agreement")
	ErrPersonalAnchorExists    = errors.Register(ModuleName, 15, "personal anchor already exists")
	ErrPersonalAnchorNotFound  = errors.Register(ModuleName, 16, "personal anchor not found")
	ErrDIDNotActive            = errors.Register(ModuleName, 17, "DID not found or not active")
	ErrUnauthorized            = errors.Register(ModuleName, 18, "unauthorized: signer does not control this DID")
	ErrInvalidSignature        = errors.Register(ModuleName, 19, "signature verification failed")
	ErrInvalidParams           = errors.Register(ModuleName, 20, "invalid params")
	ErrInvalidRequest          = errors.Register(ModuleName, 21, "invalid request")
)
