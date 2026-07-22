// SPDX-License-Identifier: Apache-2.0

//go:build phicrypto_cgo

package phicrypto

/*
#cgo CFLAGS: -I${SRCDIR}/lib
#cgo LDFLAGS: -L${SRCDIR}/lib -lphi_crypto
// The phi-crypto staticlib pulls in libm (log/log2/exp via num-bigint) and
// libdl. On macOS these live in libSystem and link implicitly; on Linux they
// must be named explicitly, after -lphi_crypto, so the single-pass linker can
// resolve the archive's references.
#cgo linux LDFLAGS: -lm -ldl
#include "phi_crypto.h"
*/
import "C"

import "unsafe"

// CGO is the production Verifier: a thin wrapper over phi-crypto through its C-ABI (built with -tags phicrypto_cgo).
type CGO struct{}

// ptrLen converts a slice into the (pointer, length) pair the C-ABI expects (empty → nil,0; pointer not retained by phi-crypto).
func ptrLen(b []byte) (*C.uint8_t, C.uintptr_t) {
	if len(b) == 0 {
		return nil, 0
	}
	return (*C.uint8_t)(unsafe.Pointer(&b[0])), C.uintptr_t(len(b))
}

// VerifySignature → phi_verify_signature.
func (CGO) VerifySignature(curve Curve, publicKey, msg, sig []byte) bool {
	pk, pkl := ptrLen(publicKey)
	m, ml := ptrLen(msg)
	s, sl := ptrLen(sig)
	return C.phi_verify_signature(C.uint8_t(curve), pk, pkl, m, ml, s, sl) == 1
}

// VerifyWebAuthn → phi_webauthn_verify (RequireUserVerification forwarded as require_uv).
func (CGO) VerifyWebAuthn(a WebAuthnAssertion) bool {
	ad, adl := ptrLen(a.AuthenticatorData)
	cd, cdl := ptrLen(a.ClientDataJSON)
	sg, sgl := ptrLen(a.Signature)
	ch, chl := ptrLen(a.Challenge)
	pk, pkl := ptrLen(a.PublicKey)
	or, orl := ptrLen([]byte(a.Origin))
	rp, rpl := ptrLen([]byte(a.RPID))
	requireUV := C.int32_t(0)
	if a.RequireUserVerification {
		requireUV = 1
	}
	return C.phi_webauthn_verify(ad, adl, cd, cdl, sg, sgl, ch, chl, pk, pkl, or, orl, rp, rpl, requireUV) == 1
}

// VerifyBBSProof → phi_bbs_verify_proof.
func (CGO) VerifyBBSProof(proof, issuerPublicKey, nonce []byte) bool {
	p, pl := ptrLen(proof)
	pk, pkl := ptrLen(issuerPublicKey)
	n, nl := ptrLen(nonce)
	return C.phi_bbs_verify_proof(p, pl, pk, pkl, n, nl) == 1
}

// VerifySemaphoreVote → phi_semaphore_verify_vote.
func (CGO) VerifySemaphoreVote(proof, issuerPublicKey, electionID, nullifier, signal []byte) bool {
	p, pl := ptrLen(proof)
	pk, pkl := ptrLen(issuerPublicKey)
	e, el := ptrLen(electionID)
	n, nl := ptrLen(nullifier)
	s, sl := ptrLen(signal)
	return C.phi_semaphore_verify_vote(p, pl, pk, pkl, e, el, n, nl, s, sl) == 1
}

// Default returns the real cgo implementation.
func Default() Verifier { return CGO{} }

// DefaultEnforces reports whether the default verifier performs real crypto verification (true in the cgo build).
func DefaultEnforces() bool { return true }
