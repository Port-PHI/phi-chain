# On-chain WebAuthn — integration design

WebAuthn signer authentication is consensus-critical. This document describes the design of the
verification seam and the requirements for enabling live passkey signers. The live router is the
remaining step; until it ships, WebAuthn envelopes are fail-closed.

## Why WebAuthn on-chain

A hardware passkey (Secure Enclave / WebCrypto non-extractable key) does not sign the transaction
sign-bytes. It signs over the envelope `authenticatorData ‖ SHA256(clientDataJSON)`, where
`clientDataJSON.challenge` is the value the relying party asked it to sign. To authenticate such a
signer the chain must reconstruct that challenge from the transaction and verify the passkey
assertion — the standard `SigVerificationDecorator` (which checks `pubKey.VerifySignature(signBytes,
sig)`) cannot do this. The signature check itself is delegated to phi-crypto (`phicrypto.Verifier` →
C-ABI); the chain never hand-rolls cryptography.

## Verification seam

- `WebAuthnSignature` is the wire envelope with strict, unambiguous `Marshal`/`Unmarshal` (magic
  `PWA1`, length-prefixed fields, no trailing bytes).
- `WebAuthnChallenge(signBytes) = SHA256(signBytes)` is the transaction binding the passkey embeds.
- `VerifyWebAuthnAssertion(verifier, env, pubKey, signBytes, origin, rpID)` builds the bound
  `phicrypto.WebAuthnAssertion` and calls the port.
- `WebAuthnDecorator` is bound to the `phicrypto.Verifier` port plus the relying-party `origin`/`rpID`,
  wired through `HandlerOptions` from `app.go`. `WebAuthnDecorator.VerifyEnvelope(pubKey, signBytes,
  sig)` is the per-signer routing decision (fail-closed, unit-tested).

`AnteHandle` is a pass-through: a WebAuthn envelope placed in a signature field is rejected downstream
by `SigVerificationDecorator` (fail-closed), and standard secp256r1/secp256k1 signing is unaffected.

## Requirements for live WebAuthn

### phi-crypto over the C-ABI (cgo)
`make phicrypto-lib` builds `libphi_crypto.a` + `phi_crypto.h` into `phicrypto/lib/`; `make build-cgo`
/ `make test-cgo` build and test with `-tags "pebbledb phicrypto_cgo"` so `phicrypto.Default()`
returns the real `CGO` verifier instead of `Disabled`. Platform artifacts are built from source, not
committed. A CI gate that regenerates `phi_crypto.h` from `ffi.rs` (cbindgen) and fails on drift is
recommended.

### Live router decorator (replaces the pass-through)
`AnteHandle` becomes a router that, for each signer of a `SigVerifiableTx`:
1. reads the signer pubkey (SEC1 P-256) and the `SignerData` (chainID, accountNumber, sequence) via
   the `AccountKeeper`, and derives `signBytes` via the injected `SignModeHandler` (mirroring
   `SigVerificationDecorator`);
2. calls `VerifyEnvelope(pubKey, signBytes, sig)`:
   - `handled == false` → leave the signer to the standard signature path (k1/r1);
   - `handled == true, err == nil` → accept (consume sig-verify gas);
   - `handled == true, err != nil` → reject the whole tx (fail-closed).

It must be deterministic and side-effect-free except the sequence increment (unchanged ordering), and
skip on `simulate` only for the crypto cost, never for the accept/reject decision.

### Relying-party values as governance params
`DefaultWebAuthnOrigin`/`DefaultWebAuthnRPID` are compile-time constants. They are promoted to a
governed param with a genesis default and a gov-only update path, so origin/rpId can change without a
binary upgrade (anti-phishing binding is consensus-relevant). Multiple allowed origins (web + native
app) are supported as a set.

### Cryptographic checks (owned by phi-crypto)
The verifier (`phi-crypto::verify_webauthn`) enforces, and the chain relies on: `type ==
"webauthn.get"`; `clientDataJSON.challenge (base64url) == WebAuthnChallenge(signBytes)`; `origin ∈
allowed`; `rpIdHash == SHA256(rpID)`; the User-Presence bit set; low-S mandatory; strict
clientDataJSON parsing; `authenticatorData` length ≥ 37. Stepped User-Verification for sensitive
operations is a separate phi-crypto requirement.

### Test matrix (consensus-critical)
k1 and r1 signatures still accepted; a WebAuthn envelope accepted only under cgo with a valid
assertion; rejected for wrong challenge, wrong origin/rpId, missing User-Presence, high-S, malformed
clientDataJSON; mixed-signer tx (one k1 + one passkey) handled; determinism across nodes; gas parity;
plus an integration test that builds a real signed tx carrying the envelope under `-tags
phicrypto_cgo`.

## Acceptance

The live router and test matrix implemented and green under `-tags "pebbledb phicrypto_cgo"`, and the relying-party params in genesis. Until then the slot stays bound-but-gated and WebAuthn envelopes are fail-closed.
