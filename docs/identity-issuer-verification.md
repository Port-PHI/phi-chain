<!-- SPDX-License-Identifier: Apache-2.0 -->
# Identity issuer verification

`x/identity` guarantees *one human = one DID*. Beyond rejecting reused DIDs and uniqueness
markers, registration is Sybil-resistant on three counts, all verified on-chain:

1. **A trusted issuer attested the registration.** The issuer is an authorized, active
   identity issuer (a `TrustedIssuer`), not an arbitrary account.
2. **The attestation is authentic.** `issuer_sig` is a valid signature by the issuer's key
   over the canonical attestation message.
3. **The registrant controls the key (proof-of-possession).** The registrant signs the
   registration with the private key for `pub_key`, and `did` is the canonical derivation
   of `pub_key`.

The signature checks run through the `phicrypto.Verifier` port and are fail-closed: the
default build's verifier rejects, so production nodes build with `-tags phicrypto_cgo`,
consistent with the rest of the chain's verification posture.

## TrustedIssuer registry

A governance-managed set of issuer DIDs and their public keys.

```proto
// proto/phi/identity/identity.proto
message TrustedIssuer {
  string did     = 1;          // did:phi:... of the issuer
  bytes  pub_key = 2;          // P-256 SEC1 public key used to verify issuer_sig
  bool   active  = 3;
}
// genesis.proto: repeated TrustedIssuer trusted_issuers = 4 [(gogoproto.nullable) = false];
// tx.proto (governance authority only):
//   MsgRegisterTrustedIssuer { string authority; TrustedIssuer issuer; }
//   MsgRevokeTrustedIssuer   { string authority; string did; }
```

Keeper: `SetTrustedIssuer` / `GetTrustedIssuer` / `IsTrustedIssuer` / `IterateTrustedIssuers`
under key prefix `0x14`, exported and imported in genesis. The first issuer is seeded in
genesis or by a governance message.

## DID derivation

`did` must be the canonical derivation of `pub_key` (byte-identical to phi-crypto
`src/did.rs` `did_from_public`, computed on-chain through the `phicrypto.DeriveDID` port
and pinned by the cross-language KAT):

```
did = "did:phi:" + hex(SHA-256(curve_tag ‖ canonical_sec1(pub_key)))   // full 32-byte digest
```

where `curve_tag` is `0x02` for secp256r1 (P-256) and `0x01` for secp256k1, and
`canonical_sec1` is the curve's canonical SEC1 encoding as emitted by RustCrypto
`to_sec1_bytes()`: **uncompressed** (65 bytes) for P-256, **compressed** (33 bytes) for
secp256k1. The key is parsed and re-encoded (compressed or uncompressed input maps to one
DID; an off-curve key is rejected). This makes the DID self-certifying: a trusted issuer
cannot bind a key to an arbitrary identifier.

## Canonical attestation message

Both `issuer_sig` and `pop_sig` are verified (curve P-256) over:

```
msg = "phi-issuer-attestation-v1" || 0x00 ||
      did || 0x00 || pub_key || 0x00 || uniqueness_hash || 0x00 || creator || 0x00 || nonce
```

`nonce` binds the attestation to a single registration (anti-replay). `issuer_sig` is the
issuer's signature over `msg`; `pop_sig` is the registrant's signature over the same `msg`,
proving possession of `pub_key`.

## Handler flow (RegisterIdentity)

1. Reject if the DID or the uniqueness marker is already used.
2. Reject if `did` is not the canonical derivation of `pub_key`.
3. Resolve `issuer_did`; require `IsTrustedIssuer` and active.
4. Verify `issuer_sig` over the canonical message (fail-closed via the port).
5. Verify `pop_sig` over the canonical message (fail-closed via the port).
6. Persist; emit events.

`MsgRotateIdentityKey` rotates the passkey of an existing DID: the current controller
authorizes (transaction signer) and `pop_sig` proves possession of the new key over a
`phi-key-rotation-v1` message. The DID identifier, controller, and uniqueness marker are
preserved.
