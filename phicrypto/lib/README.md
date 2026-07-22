# phicrypto/lib — phi-crypto C-ABI output location (final integration step)

This directory is **empty in a normal build** (only `README.md`, `.gitignore`, and the
`libphi_crypto.a.sha256` pin are tracked). The chain builds "pure Go and offline" by default and
uses [`phicrypto.Disabled`](../disabled.go) (cryptographic verification disabled = fail-safe).

The C-ABI artifacts (`libphi_crypto.a`, `phi_crypto.h`, `*.dylib`/`*.so`) are **never committed** —
they are platform-specific and built from source. Committing the `.a` blob would defeat the
open-source verifiability promise: the chain would link an opaque ~27 MB binary
that no auditor can map back to the Rust source. Instead the artifact is **built from source** and
gated by a **checked-in SHA-256 pin** (`libphi_crypto.a.sha256`).

## Build and verify (recommended: via the Makefile)

```bash
# from phi-chain/ , with the phi-crypto repo checked out as a sibling (../phi-crypto)
make phicrypto-lib          # reproducible build: cargo build --release --locked --offline
make phicrypto-lib-verify # gate the result against libphi_crypto.a.sha256
make build-cgo              # build phid with the real verifier linked
make test-cgo               # run the suite with the real C-ABI linked
```

`make phicrypto-lib` copies `libphi_crypto.a` and `phi_crypto.h` here. Regenerate the header with
`cbindgen --config cbindgen.toml --output phi_crypto.h` in the phi-crypto repo if `ffi.rs` changed.

### Manual equivalent

```bash
cd ../../phi-crypto
RUSTFLAGS="--remap-path-prefix=$(pwd)=." cargo build --release --locked --offline
cbindgen --config cbindgen.toml --output phi_crypto.h
cp target/release/libphi_crypto.a ../phi-chain/phicrypto/lib/
cp phi_crypto.h                    ../phi-chain/phicrypto/lib/
cd ../phi-chain
go build -tags "pebbledb phicrypto_cgo" ./...
```

## The SHA-256 pin (supply-chain integrity gate)

`libphi_crypto.a.sha256` holds the expected hash of the staticlib. CI (the `cgo-verifier` job)
rebuilds the staticlib from the pinned phi-crypto source and runs
[`scripts/verify-phicrypto-lib.sh`](../../scripts/verify-phicrypto-lib.sh) **before** the cgo link;
any mismatch fails the build.

The pin is **toolchain- and platform-specific**. The canonical value is the one produced in CI
(Linux x86_64, Rust pinned in [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml)); a hash
built on macOS will not match. To (re)record it, build in the canonical environment and run:

```bash
make phicrypto-lib-pin      # writes libphi_crypto.a.sha256 from the staged staticlib
```

Review the new value, then commit it. While the pin reads `UNINITIALIZED` the CI gate fails by
design — populate it once from a verified canonical build.

> The decision to "commit the artifact for offline builds" vs. "build from source at build time"
> was resolved in favor of **build-from-source + hash-pin**.
