// SPDX-License-Identifier: Apache-2.0

//go:build phicrypto_cgo && !voting_snark

package phicrypto

// CGO.VerifyDerivationVote in the cgo build WITHOUT voting_snark: the default staticlib does not export phi_voting_verify_vote_v1, so this returns false (fail-closed) rather than referencing the unlinked symbol.
func (CGO) VerifyDerivationVote(proof, issuerPublicKey, chainID, electionID, nullifier, signal []byte) bool {
	return false
}
