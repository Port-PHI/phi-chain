// SPDX-License-Identifier: Apache-2.0

package types

import "cosmossdk.io/errors"

// x/voting errors (code 1 reserved for internal errors).
var (
	ErrElectionExists     = errors.Register(ModuleName, 2, "election already exists")
	ErrElectionNotFound   = errors.Register(ModuleName, 3, "election not found")
	ErrElectionNotOpen    = errors.Register(ModuleName, 4, "election is not open")
	ErrVotingNotStarted   = errors.Register(ModuleName, 5, "voting has not started")
	ErrVotingEnded        = errors.Register(ModuleName, 6, "voting has ended")
	ErrInvalidOption      = errors.Register(ModuleName, 7, "invalid option index")
	ErrNullifierUsed      = errors.Register(ModuleName, 8, "nullifier already used in this election")
	ErrEligibilityFailed  = errors.Register(ModuleName, 9, "eligibility proof verification failed")
	ErrTemplateMissingKey = errors.Register(ModuleName, 10, "eligibility template has no issuer BBS public key")
	ErrUnauthorized       = errors.Register(ModuleName, 11, "unauthorized")
	ErrElectionHasVotes   = errors.Register(ModuleName, 12, "election already has votes")
	ErrProofTooLarge      = errors.Register(ModuleName, 13, "eligibility proof exceeds max size")
	ErrInvalidParams      = errors.Register(ModuleName, 14, "invalid params")
	ErrInvalidRequest     = errors.Register(ModuleName, 15, "invalid request")
	ErrVotingNotSound     = errors.Register(ModuleName, 16, "voting soundness gate: real tally disabled until the derivation-proof SNARK is integrated")
	ErrInvalidNullifier   = errors.Register(ModuleName, 17, "nullifier has an invalid length or shape")
)
