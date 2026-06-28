// SPDX-License-Identifier: Apache-2.0

package ante

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/Port-PHI/phi-chain/phicrypto"
)

// On-chain WebAuthn verification (consensus-critical).
//
// A hardware passkey signs over `authenticatorData ‖ SHA256(clientDataJSON)`, not
// over the transaction sign-bytes, so a WebAuthn signature CANNOT be verified by
// the standard SigVerificationDecorator (which checks pubKey.VerifySignature over
// the sign-bytes). The real verifier must therefore intercept those signers.
//
// This file ships the interface-first verification CORE (tested against the
// phicrypto.Verifier port): the wire envelope, the challenge derivation, and the
// assertion-verification call. Signature checking is delegated to phi-crypto via
// the port (never hand-rolled). Wiring the core into the live ante — replacing
// SigVerificationDecorator with a router that sends WebAuthn envelopes here and
// k1/r1 to the SDK helper — lands with the cgo verifier build; until then
// NewWebAuthnDecorator stays a pass-through and transactions use the standard
// secp256r1/secp256k1 path.

// webAuthnMagic prefixes a WebAuthn signature envelope so the future router can
// distinguish it from a raw secp256r1 signature (64 bytes) or a DER signature
// (leading 0x30) carried in the same SignerInfo signature field.
var webAuthnMagic = []byte("PWA1")

// WebAuthnSignature is the wire envelope carried in a standard signer's signature
// bytes when authenticating with a hardware passkey. The raw passkey signature,
// authenticator data and client-data JSON are verified by phi-crypto and never
// persisted ("verify and forget").
type WebAuthnSignature struct {
	AuthenticatorData []byte
	ClientDataJSON    []byte
	Signature         []byte // raw ECDSA P-256 r‖s (or DER) over authData ‖ SHA256(clientDataJSON)
}

// Marshal encodes the envelope unambiguously:
// magic ‖ u32-BE len(authData) ‖ authData ‖ u32-BE len(clientDataJSON) ‖
// clientDataJSON ‖ u32-BE len(signature) ‖ signature.
func (w WebAuthnSignature) Marshal() []byte {
	out := make([]byte, 0, len(webAuthnMagic)+12+len(w.AuthenticatorData)+len(w.ClientDataJSON)+len(w.Signature))
	out = append(out, webAuthnMagic...)
	for _, field := range [][]byte{w.AuthenticatorData, w.ClientDataJSON, w.Signature} {
		var lenBuf [4]byte
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(field)))
		out = append(out, lenBuf[:]...)
		out = append(out, field...)
	}
	return out
}

// IsWebAuthnEnvelope reports whether sig carries a WebAuthn envelope (magic prefix).
func IsWebAuthnEnvelope(sig []byte) bool {
	return len(sig) >= len(webAuthnMagic) && bytes.Equal(sig[:len(webAuthnMagic)], webAuthnMagic)
}

// UnmarshalWebAuthnSignature decodes an envelope produced by Marshal. It rejects a
// missing magic, truncated fields, or trailing bytes (strict parsing).
func UnmarshalWebAuthnSignature(b []byte) (WebAuthnSignature, error) {
	if !IsWebAuthnEnvelope(b) {
		return WebAuthnSignature{}, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "webauthn: missing envelope magic")
	}
	rest := b[len(webAuthnMagic):]
	fields := make([][]byte, 3)
	for i := range fields {
		if len(rest) < 4 {
			return WebAuthnSignature{}, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "webauthn: truncated length prefix")
		}
		n := binary.BigEndian.Uint32(rest[:4])
		rest = rest[4:]
		if uint64(len(rest)) < uint64(n) {
			return WebAuthnSignature{}, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "webauthn: field %d truncated", i)
		}
		fields[i] = rest[:n]
		rest = rest[n:]
	}
	if len(rest) != 0 {
		return WebAuthnSignature{}, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "webauthn: trailing bytes after envelope")
	}
	return WebAuthnSignature{AuthenticatorData: fields[0], ClientDataJSON: fields[1], Signature: fields[2]}, nil
}

// webAuthnChallengeDomain domain-separates the WebAuthn challenge so a passkey
// assertion captured for a Phi transaction cannot be reinterpreted as a raw sign-bytes signature in
// another context, and vice versa.
var webAuthnChallengeDomain = []byte("PHI-WEBAUTHN-v1")

// WebAuthnChallenge derives the WebAuthn challenge bound to a transaction: the domain-separated
// SHA-256 SHA256("PHI-WEBAUTHN-v1" ‖ signBytes). The passkey client must embed this exact value
// (base64url) as clientDataJSON.challenge, binding the assertion to the tx and the Phi domain.
func WebAuthnChallenge(signBytes []byte) []byte {
	h := sha256.New()
	h.Write(webAuthnChallengeDomain)
	h.Write(signBytes)
	return h.Sum(nil)
}

// VerifyWebAuthnAssertion verifies a passkey assertion for a signer via the
// phi-crypto port. publicKey is the signer's SEC1 P-256 key; signBytes are the
// transaction's standard sign-bytes; origin/rpID are the consensus-configured
// relying-party values (anti-phishing). All cryptographic checks (clientDataJSON
// type, challenge match, origin, rpIdHash, User-Presence, low-S) happen inside
// phi-crypto. With the default Disabled verifier this returns an error (fail-safe)
// until the cgo build links libphi_crypto.
func VerifyWebAuthnAssertion(verifier phicrypto.Verifier, env WebAuthnSignature, publicKey, signBytes []byte, origin, rpID string) error {
	assertion := phicrypto.WebAuthnAssertion{
		AuthenticatorData: env.AuthenticatorData,
		ClientDataJSON:    env.ClientDataJSON,
		Signature:         env.Signature,
		Challenge:         WebAuthnChallenge(signBytes),
		PublicKey:         publicKey,
		Origin:            origin,
		RPID:              rpID,
	}
	if !verifier.VerifyWebAuthn(assertion) {
		return errorsmod.Wrap(sdkerrors.ErrUnauthorized, "webauthn assertion verification failed")
	}
	return nil
}

// WebAuthnParamSource provides the GOVERNED relying-party config: the allowed-origin
// allow-list and rpId, read from consensus state so they change via governance, not a binary upgrade
// (anti-phishing binding is consensus-relevant). Implemented by the x/identity keeper. Replaces the
// former compile-time DefaultWebAuthnOrigin/RPID constants; the genesis defaults live in
// x/identity/types Params (DefaultWebAuthnAllowedOrigins / DefaultWebAuthnRPID).
type WebAuthnParamSource interface {
	WebAuthnRelyingParty(ctx sdk.Context) (allowedOrigins []string, rpID string)
}

// WebAuthnDecorator is the consensus-critical WebAuthn ante slot. It binds the phicrypto.Verifier port
// (Disabled until the cgo link) to the governed relying-party params (origin allow-list + rpId) read
// from state, so the verification seam is complete and exercised by VerifyEnvelope. It is GATED until
// mainnet: live signer interception — routing WebAuthn envelopes through VerifyEnvelope instead of the
// standard SigVerificationDecorator — lands with the cgo link (WEBAUTHN_INTEGRATION.md). Until then
// AnteHandle is a pass-through and a WebAuthn envelope placed in a signature is rejected downstream by
// SigVerificationDecorator (fail-closed); standard secp256r1/secp256k1 signing is unaffected.
type WebAuthnDecorator struct {
	verifier phicrypto.Verifier
	params   WebAuthnParamSource
}

// NewWebAuthnDecorator builds the WebAuthn decorator bound to a verifier and the governed
// relying-party param source. A nil verifier falls back to phicrypto.Default() — the fail-safe
// Disabled port without the cgo tag, the real CGO verifier with it (Default is defined in both
// builds; Disabled is not).
func NewWebAuthnDecorator(verifier phicrypto.Verifier, params WebAuthnParamSource) WebAuthnDecorator {
	if verifier == nil {
		verifier = phicrypto.Default()
	}
	return WebAuthnDecorator{verifier: verifier, params: params}
}

// VerifyEnvelope is the per-signer routing decision used by the live router (and exercised in tests).
// It reports whether sig carried a WebAuthn envelope and, if so, the result of verifying it against
// the verifier, the transaction sign-bytes, and the GOVERNED relying-party params read from state:
//   - not an envelope         -> (false, nil): handled by the standard signature path
//   - envelope, verifies      -> (true, nil)
//   - envelope, bad/!verifies -> (true, err): fail-closed
//
// The assertion is accepted only if it verifies under one of the governed allowed origins (a
// single-origin verifier is invoked per allowed origin; phishing origins are not in the set). With
// the default Disabled verifier no origin verifies, so an envelope is always fail-closed.
func (d WebAuthnDecorator) VerifyEnvelope(ctx sdk.Context, pubKey, signBytes, sig []byte) (handled bool, err error) {
	if !IsWebAuthnEnvelope(sig) {
		return false, nil
	}
	env, err := UnmarshalWebAuthnSignature(sig)
	if err != nil {
		return true, err
	}
	allowedOrigins, rpID := d.params.WebAuthnRelyingParty(ctx)
	for _, origin := range allowedOrigins {
		if VerifyWebAuthnAssertion(d.verifier, env, pubKey, signBytes, origin, rpID) == nil {
			return true, nil
		}
	}
	return true, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "webauthn assertion did not verify under any allowed origin")
}

func (d WebAuthnDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	// GATED until mainnet: live signer interception is deferred to the cgo step (see the
	// type comment and WEBAUTHN_INTEGRATION.md). Until then WebAuthn envelopes are rejected downstream
	// by the standard SigVerificationDecorator (fail-closed); standard k1/r1 signing is unaffected.
	return next(ctx, tx, simulate)
}
