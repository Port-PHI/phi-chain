# Changelog

All notable changes to **phi-chain** are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project follows
[Semantic Versioning](https://semver.org/).

## [0.5.0] - 2026-06-28

The public release of the Phi network consensus core: an independent, identity-first
blockchain on Cosmos SDK v0.53.x and CometBFT with the `phi` address prefix, built without
Ignite and fully vendored for offline builds (`GOPROXY=off -mod=vendor -tags pebbledb`).

### Added

- **`x/identity`** — `DIDDocument`, a one-human-one-DID uniqueness marker, a one-way
  bootstrap latch, and `min_identity_age`. Registration is Sybil-resistant and fail-closed:
  the DID self-certifies its key through phi-crypto's canonical `did:phi` derivation, a
  governance-managed `TrustedIssuer` attests the registration (`issuer_sig`), and the
  registrant proves possession of the key (`pop_sig`) — both verified through the
  `phicrypto.Verifier` port over a canonical attestation message. `MsgRotateIdentityKey`
  rotates a DID's passkey (controller-only, with proof-of-possession of the new key; the DID,
  controller, and uniqueness marker are preserved). A `controller→DID` secondary index backs
  one-human-one-vote eligibility lookups and is rebuilt at genesis, and validators are bound
  to a unique active DID via staking hooks. The WebAuthn relying-party configuration
  (allowed-origin list and rpId) lives in `x/identity` params, and the passkey challenge is
  domain-separated (`"PHI-WEBAUTHN-v1" ‖ signBytes`).
- **`x/coin`** — base unit `uphi` (1 PHI = 1,000,000 uphi), a fixed per-message fee table
  with a day-bucketed micro-exemption that applies to exactly one transfer per transaction
  (no bundling), and redemption-time tiered demurrage. Params are validated (a non-positive
  fee is rejected; the micro threshold and quota are bounded), stale daily micro-exemption
  quota keys are pruned in `BeginBlock`, and `phi_to_toman` must divide `UphiPerPhi` and is
  immutable while any vault holds a balance.
- **`x/institutions`** — a multi-institution registry (financial and `fx` types) that
  mints/burns against each institution's own Rial vault, with sub-organization RBAC,
  content-hash multisig for sensitive actions, day-bucketed caps, idempotent deposit/redeem
  references, and the solvency invariant `TotalSupply × 100,000 = Σ vault_balance` enforced
  in the keeper, as a registered invariant, and in tests (alongside a non-negative-vault
  invariant). It provides:
  - **fx onboarding** — an exchange applies naming an active financial guarantor
    (`MsgRequestFxEntry`); the guarantor's admin approves or declines (`MsgGuaranteeFxEntry`);
    onboarding is finalized either by the operator during the bootstrap phase or against a
    passed public governance proposal bound to the same `fx_id` (`MsgFinalizeFxEntry`).
  - **provably-backed minting** — once an institution registers a P-256 deposit-signing key
    (`MsgSetInstitutionDepositKey`), every mint carries a `deposit_proof` verified (via the
    verifier port, fail-closed) over the canonical deposit message.
  - **KYC-tier daily limits** — `kyc_tier` on mint/redeem enforces the institution's
    configured per-tier daily limit in the day-bucketed accounting.
  - **emergency stepped redemption** — a network-wide, governance-activated brake
    (`MsgSetEmergencyRedemption`) caps each holder's cumulative redemption per institution and
    relaxes over time (halted before day 30; 200 PHI from day 30; 2,000 PHI from day 60;
    unlimited from day 90); the window cannot be reset by toggling it within the period.
  - **mint controls** — attestation publishing requires a COMPLIANCE/ADMIN role (not
    OPERATOR); large mints (≥ a governed toman threshold) require aggregated ADMIN multisig;
    and protocol-level mint ceilings (per-tx and daily) are enforced even when an institution
    sets no cap of its own. Aggregated approvals are stamped with an admin-set epoch and count
    only while their signer is still an effective admin. The module also asserts the solvency
    invariants in `EndBlock` (fail-closed) as defense-in-depth.
- **`x/credentials`** — credential templates (with a bounded issuer BBS+ key), hybrid
  credential anchors, multi-party agreements, and self-signed personal anchors.
- **`x/disclosure`** — verify-only BBS+ selective-disclosure verification against anchored,
  non-revoked credentials ("verify and forget"; nothing is stored).
- **`x/voting`** — anonymous, nullifier-deduplicated polling gated by a credential template
  (distinct from on-chain governance). An eligibility proof is bound to the election,
  nullifier, and ballot choice through phi-crypto's Semaphore binding layer
  (`VerifySemaphoreVote`): `CastVote` passes the chosen option (a canonical 4-byte big-endian
  index) as the Semaphore signal, so a relay cannot re-tag a voter's choice. `CastVote` is
  accepted only when a build-tag soundness flag (`voting_snark`) is compiled in; the flag
  cannot be enabled by governance, so anonymous voting stays disabled until the zero-knowledge
  nullifier-derivation proof ships.
- **Governance** — a one-human-one-vote tally for public proposals (the quorum denominator
  counts distinct eligible controllers via `CountEligibleControllersAt`, computed once per
  block on the deterministic finalize path) and a validator-weighted tally for technical
  proposals, wired into stock `x/gov`; consensus-critical messages (software upgrade,
  consensus param updates) are routed to the validator path. An ante decorator rejects a
  transaction that would enable a governance deposit-burn flag (`BurnVoteVeto` /
  `BurnVoteQuorum` / `BurnProposalDepositPrevote`) or clear `ProposalCancelDest` while any
  institution vault holds a balance (uphi is the vault-backed deposit denom), and governance
  deposit handling is supply-neutral.
- **Custom `ante` handler** — secp256r1 key acceptance alongside secp256k1 (the policy
  recurses through nested multisig sub-keys and rejects any key that is not secp256k1/secp256r1),
  a fixed per-message fee decorator with feegrant, a per-transaction gas ceiling enforced
  before any gas-consuming decorator, and the WebAuthn verification core.
- **`phicrypto` port** — the single seam to phi-crypto (signature / WebAuthn / BBS+ /
  Semaphore verification) via C-ABI, behind the `phicrypto_cgo` build tag; the default build
  rejects all crypto verification (fail-safe) and is pure Go and offline.
- **`phid`** node binary (RPC / gRPC / LCD), genesis helpers including
  `phid genesis add-institution` (which sets the institution type explicitly), offline
  protobuf generation, and a fully vendored dependency tree. Internal imports and the proto
  `go_package` use the canonical module path `github.com/Port-PHI/phi-chain`.

### Security

- **Crypto verifier enforced on the node path at every height.** The fail-closed guard fires
  from genesis (height 0) onward, gated by an explicit flag wired only on the node path; a
  binary built without `-tags phicrypto_cgo` cannot construct the node app, so it cannot
  diverge from cgo-built validators. Production builds require the tag (documented in
  README/SECURITY); tests build the app through `app.NewApp` directly and genesis tooling
  never constructs the node app, so both are unaffected.
- **Solvency preserved across the whole slash.** Validator slashing keeps the `uphi` supply
  constant: a compensation wrapper on the staking keeper measures the actual `uphi` supply
  delta across the entire `Slash` — including the slashed portion of every active
  unbonding-delegation and redelegation — and re-mints exactly that amount to the governance
  parameter `penalty_destination`, so total supply and the solvency invariant are unchanged
  and the slashed value accrues to the operator/governance account. Governance deposit
  handling is likewise supply-neutral (vetoed/failed deposits are refunded and the
  cancellation share is routed to an account).
- **Equivocation is slashed and tombstoned.** The standard `x/evidence` module is wired (store
  key, keeper over staking + slashing and the comet info service, a BeginBlocker ordered before
  slashing/staking, and genesis init/export), so a CometBFT-reported double-sign slashes the
  validator 5%, jails, and tombstones it. That slash flows through the same compensation
  wrapper, so the burn is re-minted to `penalty_destination` and supply (and the solvency
  invariant) is preserved. An integration test drives a double-sign report through the
  BeginBlocker and asserts the 5% slash, the tombstone, conserved supply, and the invariant.
- **No governance deposit-burn can brick the peg.** An ante decorator rejects a transaction
  that would enable a deposit-burn flag or clear `ProposalCancelDest` while any institution
  vault holds a balance — unwrapping a `MsgSubmitProposal` to catch the change at submission —
  because uphi is the vault-backed denom and such a burn would shrink supply below the vault
  backing. These flags may still be enabled while every vault is empty (e.g. before launch).
- **Fee/compute coupling bounded.** The shipped genesis caps an unlimited block `MaxGas` to a
  finite default in the InitChainer (returned in `ResponseInitChain` so CometBFT adopts the
  cap from the first block), and the fixed-fee ante rejects a transaction whose declared gas
  exceeds a per-tx ceiling, so the fixed per-message fee cannot buy unbounded validator compute.
- **Multisig approval freshness.** Aggregated sensitive-action approvals are stamped with an
  admin-set epoch and count only while their signer is still an effective admin; any change to
  an institution's ADMIN set bumps the epoch and invalidates pending approvals.
- **Ballot choice bound on-chain.** `CastVote` passes the chosen option (a canonical 4-byte
  big-endian index) to the verifier as the Semaphore `signal`, so the eligibility proof must
  have been produced against `bind_nonce(election, nullifier, signal)`.
- **Governance quorum domain.** The one-human-one-vote quorum denominator counts distinct
  eligible controllers (`CountEligibleControllersAt`), matching the per-controller vote dedup,
  so multiple DIDs under one controller cannot inflate the quorum denominator.
- **Identity genesis mirrors the immutable invariants.** Genesis `Validate` enforces the
  immutable identity invariants (pub_key length, a valid bech32 controller, a known status, and
  a present/unique uniqueness marker — the one-human-one-DID anchor that `RotateIdentityKey`
  preserves), so a curated genesis cannot seed controller-spoofed or malformed DIDs. It admits
  rotated identities (a rotated DID keeps its identifier across a key change), and
  self-certification stays enforced at registration time; a regression test covers the
  register → rotate → export → fresh `InitGenesis` round-trip.
- **Param/input hardening and anti-replay.** `phi_to_toman` must divide `UphiPerPhi`;
  `deposit_ref` / `redeem_ref` / `fx_*` are length-bounded in `ValidateBasic`; institution
  mint/redeem require a non-empty reference and record an idempotency marker for both
  directions; issuer attestation nonces are persisted and single-use; and the key-type policy
  recurses through a nested multisig's sub-keys, rejecting any key that is not
  secp256k1/secp256r1.
- **Complete genesis round-trip.** `ExportGenesis` / `InitGenesis` carry the deposit/redeem
  idempotency markers, cap counters, accumulated approvals, issuer single-use nonce markers, and
  the validator↔DID bindings (prefix-confined); coin genesis validates CoinAge addresses/buckets;
  coin/credentials/disclosure/voting `InitGenesis` re-run `gs.Validate()`; and institutions
  genesis dedupes role grants. The validator↔DID bijection is enforced at genesis (both
  directions; the bound DID must exist and be ACTIVE).
- **Invariants registered with `x/crisis`.** The app registers module invariants by iterating
  the invariant-bearing modules, so the `solvency` and `non-negative-vault` invariants are
  enforced at runtime and on the keeper write path on every mint/redeem; a legitimate
  low-liquidity attestation is an allowed state, surfaced as a non-halting health metric/event.
- **Supply chain.** The phi-crypto C-ABI staticlib is built from vendored source and verified
  against a checked-in SHA-256 pin before the cgo link, and every third-party GitHub Action in
  `.github/workflows` is pinned to a full commit SHA with a `.github/dependabot.yml` (scoped to
  the `github-actions` ecosystem) keeping them current.

---

*Homaan Smart Data Co. — portphi.com*
