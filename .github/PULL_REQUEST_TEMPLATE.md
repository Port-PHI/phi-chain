<!-- SPDX-License-Identifier: Apache-2.0 -->

## Summary

<!-- What does this PR change and why? -->

## Checklist

- [ ] Commit messages follow Conventional Commits (`feat(scope): ...`, `fix(scope): ...`, ...).
- [ ] `GOPROXY=off GOFLAGS=-mod=vendor go test -tags pebbledb ./...` passes.
- [ ] `go vet -tags pebbledb ./...` is clean and `gofmt -l .` is empty.
- [ ] Invariants (if touched) are enforced in the keeper **and** a registered `Invariant` **and** a test.
- [ ] New/changed source files carry the `// SPDX-License-Identifier: Apache-2.0` header; comments are concise English; exported APIs have godoc.
- [ ] No secrets, keys, or credentials added (code, tests, or comments); no raw personal data on chain.
- [ ] No hand-rolled cryptography — sensitive primitives go through the `phicrypto.Verifier` port; consensus-/crypto-sensitive changes flagged for extra review.
- [ ] If a proto message changed: regenerated via `scripts/protocgen.sh`; if a new import was introduced, `go.mod`/`vendor/` updated so offline builds keep working.
- [ ] Commits are signed off (DCO): `git commit -s`.
