# Security Policy — phi-chain

phi-chain is the consensus core of the Phi network — an identity-first blockchain whose purpose is to
let a person prove who they are, and what they are entitled to claim, **without exposing their raw
personal data.** Security is therefore not a feature of this project; it *is* the project. We take
vulnerabilities seriously and appreciate responsible disclosure.

## Reporting a Vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

Report privately to **security@portphi.com**.

Please include:

- A clear description of the issue and its impact.
- Steps to reproduce — a failing test, a transaction, or a minimal scenario where possible.
- The affected component or module (for example `identity`, `credentials`, `disclosure`, `voting`,
  `governance`, `coin`, `institutions`, the `ante` handler, or `phicrypto`) and the commit you tested
  against.
- Any suggested remediation.

We aim to acknowledge reports promptly and will coordinate a fix and a disclosure timeline with you.
Please give us reasonable time to remediate before any public disclosure; we are glad to credit
reporters who follow coordinated disclosure.

## Our security model

Phi is engineered so that strong guarantees hold by construction. Reports that demonstrate a way to
break any of the following are especially valuable.

- **Privacy by design — "verify and forget".** Only hashes, decentralized identifiers (DIDs), and
  signatures are ever written to chain state. Raw personal data and biometric material never leave the
  user's device and must never reach the ledger. Any path that places raw personal or biometric data
  into chain state is a critical concern.

- **Identity integrity — one human, one identity.** A Phi identity must be unforgeable and unique to a
  real person. Anything that lets an identity be duplicated, forged, impersonated, or Sybil-inflated
  is in scope.

- **Fail-safe cryptography.** All sensitive cryptography — selective-disclosure proofs, WebAuthn
  assertions, and signatures — is delegated to [phi-crypto](https://github.com/Port-PHI/phi-crypto)
  and is never hand-rolled here. Verifiers **fail closed**: they reject on any doubt. Anything that
  lets an invalid signature, assertion, or proof verify as valid is in scope.

- **Selective-disclosure soundness and unlinkability.** A presented proof must reveal only the claim
  it asserts, two presentations of one credential must not be correlatable, and a proof must not be
  forgeable or replayable against a different context.

- **Consensus soundness and determinism.** Anything that lets a node accept an invalid block or
  transaction, or causes nodes to diverge (non-determinism), is in scope.

- **Authentication and key custody.** Device-bound keys are non-extractable by design; the key-type
  acceptance policy (secp256r1 alongside secp256k1) and the WebAuthn authentication path are
  consensus-critical. Validator keys must never live on disk in production (use a remote signer).

Cryptographic primitives live in [phi-crypto](https://github.com/Port-PHI/phi-crypto) and are verified
there; soundness issues in the primitives themselves should also be reported against that repository.

## No secrets in the repository

No keys, secrets, or credentials are ever committed to this repository — not in code, tests, or
comments. If you ever find a committed secret, please report it through the channel above.

---

*Homaan Smart Data Co. — portphi.com*
