//go:build voting_snark

// SPDX-License-Identifier: Apache-2.0

package keeper

// VotingSoundnessEnforced is true in the voting_snark build: the audited derivation-proof circuit is
// integrated, so the Semaphore layer is Sybil-resistant and CastVote may reach a real tally
// See soundness.go for the default (binding-only) build and the full rationale.
const VotingSoundnessEnforced = true
