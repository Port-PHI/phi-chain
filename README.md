<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/phi-mark-dark.svg">
  <img src="assets/phi-mark.svg" alt="Phi (φ)" width="170">
</picture>

# PHI — the identity blockchain

[![CI](https://github.com/Port-PHI/phi-chain/actions/workflows/ci.yml/badge.svg)](https://github.com/Port-PHI/phi-chain/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](./LICENSE)

**English** · [**فارسی**](./README.fa.md)

Homaan Smart Data Co. · [portphi.com](https://portphi.com)

</div>

---

**Proof of personhood and verifiable identity for the internet** · *verify and forget*

## What Phi is

**Phi** is a public, identity-first blockchain — infrastructure that gives every person a single,
unforgeable digital identity they own and control, and lets them prove who they are and what they are
entitled to claim, **without ever exposing their raw personal data.**

A person is verified **once**. From that moment they hold a **Phi identity** — a decentralized
identifier (DID) secured by a non-extractable key on their own device — and can authenticate, sign,
and present verifiable credentials anywhere on the internet. The network's founding principle is
**verify and forget**: only a cryptographic hash, the DID, and a signature are ever written
on-chain. No biometric template, no document image, no raw personal data ever leaves the user's
device or reaches the ledger.

Phi exists to answer a question the internet has never answered well — *is there a real, unique human
behind this action?* — and to answer it while preserving the person's privacy and ownership of their
own identity.

## What the network does

Phi is an identity and authentication network. Its core is built around four capabilities:

- **Proof of personhood — one human, one identity.** Each person establishes a single Phi identity
  through strong verification and a privacy-preserving uniqueness check, so an identity cannot be
  duplicated or forged. This makes Phi resistant to Sybil attacks, bots, and impersonation by design.

- **Authentication — "Sign in with Phi".** Passwordless, phishing-resistant sign-in backed by
  device-bound passkeys (WebAuthn / secure-enclave keys). Proving "a real, unique human is present"
  becomes a single tap — no passwords, no shared secrets, no central honeypot of credentials.

- **Verifiable credentials and documents.** Issue, hold, and present tamper-evident credentials and
  signed documents anchored to a Phi identity — diplomas, licenses, memberships, agreements, and more
  — each cryptographically verifiable and owned by the holder.

- **Selective disclosure — prove without revealing.** Using unlinkable zero-knowledge proofs a holder
  can prove a claim — *"over 18", "a resident of this city", "a verified human"* — without revealing
  the data behind it, and two presentations of the same credential cannot be correlated. The holder
  decides exactly what is shown and what stays private.

Everything is **self-sovereign and verifiable**: the user holds the keys, the identity lives on the
user's device, and the protocol that secures it is open, verifiable, and permissionless.

> **A native value layer.** Phi also includes a native unit of value as **one component** of the
> network — a settlement layer operated by licensed institutions. It is one branch built on top of a
> trusted identity; identity, authentication, and credentials remain the heart of the protocol.

## Architecture

phi-chain is the consensus core of the Phi network: an independent, sovereign blockchain (Cosmos SDK
+ CometBFT, `phi` address prefix) that every node runs to reach agreement. Its modules are organized
around identity first.

| Module | Role |
|---|---|
| `x/identity` | **The core.** Each person's Phi identity (DID), the one-human-one-identity uniqueness guarantee, trusted-issuer attestation, and proof of possession of the device-bound key. |
| `x/credentials` | Verifiable credentials and signed documents/agreements anchored to a Phi identity. |
| `x/disclosure` | Privacy-preserving **selective disclosure** — present a proof of a claim without revealing the data behind it. |
| `x/voting` | Anonymous, one-person-one-vote participation. |
| `x/governance` | One-human-one-vote network governance. |
| `x/coin` · `x/institutions` | The native value and settlement layer (one component of the network). |
| custom `ante` | Device-bound key acceptance — passkeys / secp256r1 alongside secp256k1 — and WebAuthn authentication, so a human, not just a key, stands behind a transaction. |

All sensitive cryptography (selective-disclosure proofs, WebAuthn, signatures) is delegated to
[**phi-crypto**](https://github.com/Port-PHI/phi-crypto), the project's single, audited-crate-based
cryptographic core. The chain **never hand-rolls cryptography.**

> The Phi platform is being delivered in stages; capabilities are rolling out across upcoming updates,
> each shipped only when it is complete, tested, and verifiable.

## Privacy by design — "verify and forget"

Phi is built so that the network can confirm a fact about a person **without ever holding the data
that proves it**:

1. A person is verified once, on their own device.
2. Only a hash, the DID, and signatures are written on-chain — never biometric or raw personal data.
3. Claims are presented as zero-knowledge proofs; the verifier learns the answer, not the underlying
   information.

The result is an identity layer that is at once **unforgeable** (a real, unique human stands behind
each identity) and **private** (the person owns and minimizes what is revealed).

## Build & run

The repository is fully self-contained and builds **offline** — every dependency is vendored under
`vendor/`. The default database backend is pebbledb (`-tags pebbledb`, handled by the Makefile).

```bash
make build                 # → build/phid
make test                  # run the test suite
make test-invariants       # protocol invariants

# Local single-node devnet
phid init my-node --chain-id phi-testnet-1
phid keys add validator --keyring-backend test
phid genesis add-genesis-account validator 100000000uphi --keyring-backend test
phid genesis gentx validator 70000000uphi --chain-id phi-testnet-1 --keyring-backend test
phid genesis collect-gentxs
phid start
```

Node ports: RPC/WS `26657` · gRPC `9090` · REST/LCD `1317`.

To link the live phi-crypto verifier for real on-chain proof and signature verification, build with
`make build-cgo` / `make test-cgo` (`-tags "pebbledb phicrypto_cgo"`). A production validator binary
is built with this tag so the real cryptographic core is linked.

## Built on

Phi is built on industry-standard open-source infrastructure: **Cosmos SDK**, **CometBFT**, and
**Protocol Buffers / gRPC**. Full attribution for these components is in [`NOTICE`](./NOTICE).

## Security & contributing

We take security seriously and welcome responsible disclosure — see [`SECURITY.md`](./SECURITY.md)
for how to report a vulnerability privately.
To contribute, read [`CONTRIBUTING.md`](./CONTRIBUTING.md); project stewardship is described in
[`GOVERNANCE.md`](./GOVERNANCE.md).

## License

[Apache License 2.0](./LICENSE) — © 2026 Homaan Smart Data Co. All rights reserved.

Designed and invented by **A.Mooraeyan**. The Phi protocol and the original source in this repository
are the intellectual property of Homaan Smart Data Co.; all copyright and patent rights are owned and
reserved by the company, which licenses the software for public use, study, and redistribution under
Apache-2.0. See [`NOTICE`](./NOTICE) for the full ownership, patent, and trademark statement.

---

*Homaan Smart Data Co. — [portphi.com](https://portphi.com)*
