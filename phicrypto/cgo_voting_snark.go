// SPDX-License-Identifier: Apache-2.0

//go:build phicrypto_cgo && voting_snark

package phicrypto

/*
// The voting_snark-featured superset staticlib exports phi_voting_verify_vote_v1 in addition to the
// default C-ABI. This file is its own cgo translation unit: it includes the featured header only, so it
// does not collide with cgo.go's phi_crypto.h (both share the PHI_CRYPTO_H guard, but per-file cgo
// preambles are compiled separately). The -L/-lphi_crypto link flags come from cgo.go and accumulate
// package-wide, resolving both the base exports and phi_voting_verify_vote_v1 against the superset lib.
#cgo CFLAGS: -I${SRCDIR}/lib
#include "phi_crypto_voting_snark.h"
*/
import "C"

// VerifyDerivationVote → phi_voting_verify_vote_v1: the composed anonymous-vote proof (one-credential-one-nullifier-per-election); nullifier must equal the 48-byte compressed G1 bound inside the proof, else fail-closed.
func (CGO) VerifyDerivationVote(proof, issuerPublicKey, chainID, electionID, nullifier, signal []byte) bool {
	p, pl := ptrLen(proof)
	pk, pkl := ptrLen(issuerPublicKey)
	c, cl := ptrLen(chainID)
	e, el := ptrLen(electionID)
	n, nl := ptrLen(nullifier)
	s, sl := ptrLen(signal)
	return C.phi_voting_verify_vote_v1(p, pl, pk, pkl, c, cl, e, el, n, nl, s, sl) == 1
}
