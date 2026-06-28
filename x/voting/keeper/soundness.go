//go:build !voting_snark

// SPDX-License-Identifier: Apache-2.0

package keeper

// VotingSoundnessEnforced reports whether the audited zero-knowledge derivation-proof circuit —
// proving nullifier = H(secret, election) for a signed-claim secret — is built into this binary.
//
// Until that SNARK ships in phi-crypto and is wired here, the Semaphore layer is binding-only and NOT
// Sybil-resistant: one credential can mint many fresh nullifiers and vote more than once. So CastVote
// must not reach a real tally. This mirrors phicrypto.DefaultEnforces: it is false in the
// default build and CANNOT be enabled by governance; flip it only by building with -tags voting_snark
// once the vetted circuit lands (with maintainer review).
const VotingSoundnessEnforced = false
