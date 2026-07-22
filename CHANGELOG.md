# Changelog

All notable changes to **phi-chain** are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project follows
[Semantic Versioning](https://semver.org/).

## [1.0.0] — Initial public release

The first public release of the Phi network consensus core: an independent, identity-first
blockchain built on Cosmos SDK and CometBFT with the `phi` address prefix. The tree builds and
tests fully offline from a vendored dependency set (`GOPROXY=off -mod=vendor -tags pebbledb`), and
all sensitive cryptography is delegated to [phi-crypto](https://github.com/Port-PHI/phi-crypto)
through a single verifier port — the chain never hand-rolls cryptography.

### Modules

- **`x/identity`** — one human, one decentralized identifier (DID). Registration is
  Sybil-resistant and fail-closed: the DID self-certifies its key through phi-crypto's canonical
  `did:phi` derivation, a governance-managed trusted issuer attests the registration, and the
  registrant proves possession of the key. A uniqueness marker enforces one-human-one-DID and is
  preserved across key rotation; the WebAuthn relying-party configuration (allowed origins and
  rpId) is governed on-chain.
- **`x/coin`** — the native unit `uphi` (1 PHI = 1,000,000 uphi), a fixed per-message fee table
  with a bounded daily micro-exemption, and redemption-time tiered demurrage. The fixed peg
  parameter must divide the base unit and is immutable while any institution vault holds a balance.
- **`x/institutions`** — a multi-institution registry (financial and `fx` types) that mints and
  burns against each institution's own Rial vault, with sub-organization RBAC, content-hash
  multisig for sensitive actions, day-bucketed and KYC-tier caps, idempotent deposit/redeem
  references, provably-backed minting against a registered deposit-signing key, and a
  governance-activated emergency stepped-redemption brake. The solvency invariant
  `TotalSupply × 100,000 = Σ vault_balance` and a non-negative-vault invariant are enforced in the
  keeper, as registered invariants, and in tests.
- **`x/credentials`** — credential templates with a bounded issuer BBS+ key, hybrid credential
  anchors, multi-party agreements, and self-signed personal anchors.
- **`x/disclosure`** — verify-only BBS+ selective-disclosure verification against anchored,
  non-revoked credentials. Nothing is stored: "verify and forget".
- **`x/voting`** — anonymous, nullifier-deduplicated polling gated by a credential template and
  distinct from on-chain governance. Each ballot binds the eligibility proof to the election,
  nullifier, and chosen option through phi-crypto's Semaphore binding layer, so a relay cannot
  re-tag a voter's choice. The zero-knowledge nullifier-derivation proof is implemented and
  integrated: the release build (`-tags voting_snark`) enables ZK-verified anonymous voting, while
  the default build compiles in a fail-closed stub that rejects every ballot and cannot be enabled
  by governance.
- **`x/governance`** — one-human-one-vote tallying for public proposals (the quorum denominator
  counts distinct eligible controllers) and validator-weighted tallying for technical proposals,
  wired into the standard governance module; consensus-critical messages route to the
  validator-weighted path.

### Consensus & execution

- A custom `ante` handler accepts device-bound passkeys (secp256r1) alongside secp256k1 — recursing
  through nested multisig sub-keys and rejecting any other key type — and provides the fixed
  per-message fee decorator with feegrant, a per-transaction gas ceiling enforced before any
  gas-consuming decorator, and the WebAuthn verification core.
- The `phicrypto` port is the single seam to phi-crypto (signature, WebAuthn, BBS+, and Semaphore
  verification) over a C-ABI behind the `phicrypto_cgo` build tag. The default build is pure Go,
  offline, and fail-safe: it rejects all cryptographic verification, and a binary built without the
  tag cannot construct the node application, so it cannot diverge from cgo-built validators.
- The `phid` node binary exposes RPC, gRPC, and LCD endpoints and ships genesis tooling, offline
  protobuf generation, and a fully vendored dependency tree.

### Guarantees

- **Privacy by design — "verify and forget".** Only hashes, DIDs, and signatures reach chain
  state; raw personal and biometric data never leave the user's device.
- **Solvency preserved across slashing and governance.** Validator slashing (including
  double-sign equivocation, which is slashed and tombstoned via `x/evidence`) keeps the `uphi`
  supply constant by re-minting the measured slash delta to a governance destination, and
  governance deposit handling is supply-neutral. An `ante` guard rejects any change that would
  enable a governance deposit-burn while a vault holds a balance, so the peg cannot be bricked.
- **Bounded fee/compute coupling.** Genesis caps block `MaxGas` from the first block, and the
  fixed-fee `ante` rejects a transaction whose declared gas exceeds a per-transaction ceiling.
- **Complete, validated genesis round-trip.** Export and import carry idempotency markers, cap
  counters, accumulated approvals, single-use issuer nonces, and the validator↔DID bijection;
  module genesis re-runs validation and enforces the immutable identity invariants.
- **Reproducible, verifiable supply chain.** The phi-crypto C-ABI staticlib is built from vendored
  source and verified against a checked-in SHA-256 pin before the cgo link, and every third-party
  GitHub Action is pinned to a full commit SHA.

---

*Homaan Smart Data Co. — portphi.com*
