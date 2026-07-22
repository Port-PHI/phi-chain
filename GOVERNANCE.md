# Governance

This document describes the governance of the **phi-chain** open-source project — how this repository
is stewarded, how decisions are made, how changes are reviewed, and how releases are cut.

It concerns the **project**, not the protocol. It is separate from the *on-chain* governance of the
Phi network — the one-human-one-vote and validator-weighted mechanisms implemented by the `x/gov` and
`governance` tally and the `x/voting` module — which is a feature of the blockchain itself and is
documented with those modules.

## Mission

phi-chain is the consensus core of an identity-first network: infrastructure that lets a person prove
who they are, and what they are entitled to claim, without exposing their raw personal data. Because
the integrity and privacy of people's identities depend on this code, the project is run to a
deliberately high standard of correctness, transparency, and review.

## Principles

- **Open and verifiable.** The protocol every node runs is public and auditable — *the network does
  not claim; it shows.* All work happens in the open.
- **Privacy first.** Changes preserve the "verify and forget" guarantee: only hashes, DIDs, and
  signatures reach chain state.
- **Never hand-roll cryptography.** Sensitive primitives are delegated to
  [phi-crypto](https://github.com/Port-PHI/phi-crypto) through the `phicrypto.Verifier` port.
- **Correctness over speed.** Security- and consensus-relevant changes are shipped only when they are
  complete, tested, and reviewed.

## Stewardship

phi-chain is stewarded by **Homaan Smart Data Co.** under the
[`Port-PHI`](https://github.com/Port-PHI) organization. During the pre-mainnet phase the project
follows a maintainer-led model: the steward sets direction and has final say on changes, with all
work happening in the open. As the network matures, governance is intended to broaden toward the
community and the network's on-chain mechanisms.

## Roles

- **Steward (Homaan Smart Data Co.)** — owns the project's direction, the release process, and final
  decisions; holds the project's intellectual-property and trademark rights.
- **Maintainers** — review and merge changes, uphold the standards in this document and
  [`CONTRIBUTING.md`](CONTRIBUTING.md), and triage security reports. Maintainers are listed via
  [`CODEOWNERS`](.github/CODEOWNERS).
- **Contributors** — anyone who proposes an issue or a pull request. Sustained, high-quality
  contributions may lead to a maintainer invitation at the steward's discretion.

## Decision-making

- Changes land via pull requests reviewed under [`CODEOWNERS`](.github/CODEOWNERS); every change is
  reviewed before it merges to `main`.
- **Consensus- and security-sensitive changes** — module state machines, the `ante` handler (key-type
  acceptance, authentication, fees), governance tally rules, protocol invariants, and anything
  affecting determinism — require explicit maintainer review and a clear written rationale.
- Substantial design changes should be raised as an issue first, so the approach can be discussed
  before implementation.
- Disagreements are resolved through discussion on the issue or pull request; where consensus is not
  reached, the steward decides.

## Review & quality bar

- No change merges without tests; pull requests that reduce coverage are not merged.
- The build and test suite must be green and fully offline (`GOPROXY=off -mod=vendor`, `-tags
  pebbledb`); the source stays `gofmt`- and `go vet`-clean.
- Continuous integration must pass on every pull request before merge.

## Releases

- Versioning follows [SemVer](https://semver.org/); user-visible changes are recorded in
  [`CHANGELOG.md`](CHANGELOG.md).
- Release tags are signed, and a release must build and test green offline.
- Dependencies are vendored for reproducible, verifiable builds.
- Every release is complete, tested, and builds green offline before it ships; the consensus core
  is released as a complete module set.

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for coding standards, the offline build/test workflow,
Conventional Commits, and the developer sign-off.

## Security

Report vulnerabilities privately as described in [`SECURITY.md`](SECURITY.md)
(**security@portphi.com**) — never via a public issue.

## License

phi-chain is licensed under the Apache License, Version 2.0 (see [`LICENSE`](LICENSE) and
[`NOTICE`](NOTICE)). The Phi protocol and the original source in this repository are the intellectual
property of Homaan Smart Data Co.

---

*Homaan Smart Data Co. — portphi.com*
