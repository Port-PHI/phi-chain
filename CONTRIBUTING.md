# Contributing to phi-chain

Thank you for your interest in contributing. phi-chain is the **consensus core** of the Phi network —
an identity-first blockchain that lets a person prove who they are, and what they are entitled to
claim, without exposing their raw personal data. *The network does not claim; it shows*, and open,
verifiable code is part of that promise. Because a bug here affects the integrity and privacy of
people's identities across every node, the apps, and the web, contributions are held to a high bar.

## Scope

This repository contains the public protocol code every node runs to reach consensus: an independent
Cosmos SDK + CometBFT chain with the `phi` address prefix and the Phi modules — `identity`,
`credentials`, `disclosure`, `voting`, `governance`, `coin`, and `institutions` — plus a custom `ante`
handler for device-bound key acceptance and WebAuthn authentication. Company backend services,
websites, and apps are **not** part of this repository.

## The non-negotiable rule: never hand-roll cryptography

All sensitive cryptography — selective-disclosure proofs (BBS+), WebAuthn, and DID/signature
verification — is delegated to [phi-crypto](https://github.com/Port-PHI/phi-crypto) through the
`phicrypto.Verifier` port. Chain code is written against that interface and tested with the
`phicrypto.Fake` verifier; the real implementation is linked via cgo behind the `phicrypto_cgo` build
tag. New code that reimplements a cryptographic primitive instead of calling the port will be
rejected.

## Code standards

- Go (toolchain per `go.mod`), with the standard Cosmos SDK module layout: protobuf state, Msg/Query
  services, a keeper that enforces invariants, events, and genesis.
- Protocol invariants are enforced in **three** places — the keeper, a registered `Invariant`, and a
  test (for example `go test ./x/institutions/... -run Invariant`).
- `gofmt`-clean and `go vet`-clean.
- Comments in **English**, concise and standard (godoc on exported APIs); names, identifiers, APIs,
  and logs in English. Every source file carries the `// SPDX-License-Identifier: Apache-2.0` header.
- **No raw personal data on chain** — only hashes, DIDs, and signatures ("verify and forget").
- Consensus-critical logic (key-type acceptance, the WebAuthn path, tally rules, fees, invariants)
  requires extra maintainer review.

## Build & test (fully offline / vendored)

All dependencies are committed under `vendor/`; the build never downloads anything. The `pebbledb`
backend tag is required.

```bash
GOPROXY=off GOFLAGS=-mod=vendor go build -tags pebbledb ./...
GOPROXY=off GOFLAGS=-mod=vendor go test  -tags pebbledb ./...
GOPROXY=off GOFLAGS=-mod=vendor go vet   -tags pebbledb ./...
gofmt -l .
```

To build and test with the real phi-crypto verifier linked:

```bash
make build-cgo    # or: make test-cgo   (-tags "pebbledb phicrypto_cgo")
```

Protobuf is regenerated offline (the proto dependencies are vendored locally):

```bash
PATH="$(go env GOPATH)/bin:$PATH" GOPROXY=off bash scripts/protocgen.sh
```

Only if you add a proto message that introduces a **new** import do you need a one-time online
`go mod tidy && go mod vendor`.

**No change is merged without tests.** Pull requests that reduce coverage will not be merged.

## Branches & commits

- Base work on `main`. Branch naming: `feat/…`, `fix/…`, `docs/…`, `test/…`, `chore/…`. Keep pull
  requests small and focused — one logical change each.
- [Conventional Commits](https://www.conventionalcommits.org/): `<type>(<scope>): <summary>` with
  types `feat|fix|docs|test|refactor|chore|perf|ci` and a module scope — for example
  `feat(identity): add issuer rotation`, `fix(disclosure): reject mismatched nonce`.
- Sign off your commits (DCO): `git commit -s`.
- Every pull request should describe what changed and why, include tests, and note any consensus- or
  privacy-affecting implications.

## Security issues

**Do not open public issues for security vulnerabilities.** See [`SECURITY.md`](./SECURITY.md) —
report privately to **security@portphi.com**.

## License

By submitting a contribution you agree it is licensed under this repository's
[Apache License 2.0](./LICENSE) (per Section 5 of the license). The Phi protocol and the original
source in this repository are the intellectual property of Homaan Smart Data Co.

---

*Homaan Smart Data Co. — portphi.com*
