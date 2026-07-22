//go:build !voting_snark

// SPDX-License-Identifier: Apache-2.0

package keeper

// VotingSoundnessEnforced reports whether the zero-knowledge derivation-proof circuit — proving nullifier = H(secret, election) for a signed-claim secret — is built into this binary.
const VotingSoundnessEnforced = false
