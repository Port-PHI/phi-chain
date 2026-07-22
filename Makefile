# SPDX-License-Identifier: Apache-2.0
#!/usr/bin/make -f
# Makefile for the Phi chain (phi-chain)

BINARY  = phid
BUILDDIR ?= $(CURDIR)/build
GOBIN ?= $(shell go env GOPATH)/bin
# phi-crypto source tree (sibling repo) - source for the C-ABI staticlib + header.
PHICRYPTO_DIR ?= $(CURDIR)/../phi-crypto

VERSION := $(shell git describe --tags 2>/dev/null || echo "dev")
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")

ldflags = -X github.com/cosmos/cosmos-sdk/version.Name=phi \
          -X github.com/cosmos/cosmos-sdk/version.AppName=$(BINARY) \
          -X github.com/cosmos/cosmos-sdk/version.Version=$(VERSION) \
          -X github.com/cosmos/cosmos-sdk/version.Commit=$(COMMIT)

# The pebbledb tag is required: Phi's default database backend is pebbledb
# (goleveldb has a read-after-write issue under Go 1.25+).
BUILD_TAGS := pebbledb
BUILD_FLAGS := -tags '$(BUILD_TAGS)' -ldflags '$(ldflags)'

# The phi-crypto C-ABI staticlib must be REPRODUCIBLE from source: pinned deps
# (--locked), no network (--offline, vendored crates), and path-independent object files
# (--remap-path-prefix) so the SHA-256 pin is stable across checkout locations on one platform.
PHICRYPTO_STATICLIB := phicrypto/lib/libphi_crypto.a
PHICRYPTO_PIN       := phicrypto/lib/libphi_crypto.a.sha256

.PHONY: all build install test test-invariants vet proto-gen lint clean tidy \
        phicrypto-lib phicrypto-lib-verify phicrypto-lib-pin build-cgo test-cgo \
        phicrypto-lib-snark build-cgo-snark test-cgo-snark

all: vet test build

## build: compile the phid binary into build/.
build:
	@echo ">> building $(BINARY)"
	@go build $(BUILD_FLAGS) -o "$(BUILDDIR)/$(BINARY)" ./cmd/phid

## install: install phid into GOBIN.
install:
	@echo ">> installing $(BINARY) -> $(GOBIN)"
	@go install $(BUILD_FLAGS) ./cmd/phid

## test: run unit tests for all modules.
test:
	@go test ./...

## test-invariants: test the solvency invariants (multi-institution model).
test-invariants:
	@go test ./x/institutions/... -run Invariant -v
	@go test ./x/coin/... -run Invariant -v

## phicrypto-lib: reproducibly build phi-crypto's C-ABI staticlib + header into phicrypto/lib/
## (prerequisite for the phicrypto_cgo build). Regenerate the header with cbindgen in
## $(PHICRYPTO_DIR) if ffi.rs changed. Uses --locked --offline (vendored, pinned deps) so the
## artifact is reproducible from source; verify it afterwards with `make phicrypto-lib-verify`.
## The remap flag is passed via CARGO_ENCODED_RUSTFLAGS (Cargo splits it only on \x1f, never on
## spaces) so a checkout path containing spaces does not break the single --remap-path-prefix flag.
phicrypto-lib:
	@echo ">> building phi-crypto C-ABI staticlib from $(PHICRYPTO_DIR) (reproducible)"
	@cd "$(PHICRYPTO_DIR)" && CARGO_ENCODED_RUSTFLAGS="--remap-path-prefix=$$(pwd)=." cargo build --release --locked --offline
	@cp "$(PHICRYPTO_DIR)/target/release/libphi_crypto.a" phicrypto/lib/
	@cp "$(PHICRYPTO_DIR)/phi_crypto.h" phicrypto/lib/
	@echo ">> phicrypto/lib/ ready (libphi_crypto.a + phi_crypto.h)"

## phicrypto-lib-verify: gate the staged staticlib against the checked-in SHA-256 pin.
phicrypto-lib-verify:
	@bash ./scripts/verify-phicrypto-lib.sh "$(PHICRYPTO_STATICLIB)" "$(PHICRYPTO_PIN)"

## phicrypto-lib-pin: (re)record the SHA-256 pin from the staged staticlib. Run ONLY in the
## canonical build environment (see the pin file header); review the new value before committing.
phicrypto-lib-pin:
	@test -f "$(PHICRYPTO_STATICLIB)" || { echo "error: $(PHICRYPTO_STATICLIB) not found (run 'make phicrypto-lib' first)"; exit 1; }
	@hash="$$(if command -v sha256sum >/dev/null 2>&1; then sha256sum "$(PHICRYPTO_STATICLIB)"; else shasum -a 256 "$(PHICRYPTO_STATICLIB)"; fi | awk '{print $$1}')"; \
	{ \
	  echo "# SHA-256 pin for the phi-crypto C-ABI staticlib (libphi_crypto.a) — supply-chain"; \
	  echo "# integrity gate. Regenerate with 'make phicrypto-lib-pin' in the"; \
	  echo "# canonical environment (Linux x86_64, the Rust toolchain pinned in CI). The macOS hash"; \
	  echo "# will not match the Linux/CI build. Format: \"<sha256>  libphi_crypto.a\"."; \
	  echo "$$hash  libphi_crypto.a"; \
	} > "$(PHICRYPTO_PIN)"; \
	echo ">> wrote $(PHICRYPTO_PIN): $$hash"

## build-cgo: build phid with the real phi-crypto verifier linked (run 'make phicrypto-lib' first).
build-cgo:
	@echo ">> building $(BINARY) with phicrypto_cgo (real on-chain crypto verification)"
	@go build -tags 'pebbledb phicrypto_cgo' -ldflags '$(ldflags)' -o "$(BUILDDIR)/$(BINARY)" ./cmd/phid

## test-cgo: run the suite with the real phi-crypto C-ABI linked (run 'make phicrypto-lib' first).
test-cgo:
	@go test -tags 'pebbledb phicrypto_cgo' ./...

## phicrypto-lib-snark: build the voting_snark-FEATURED C-ABI staticlib (a superset that also exports
## phi_voting_verify_vote_v1) and stage it plus BOTH headers into phicrypto/lib/. This is a SEPARATE,
## test/audit-only artifact: it overwrites phicrypto/lib/libphi_crypto.a LOCALLY (same -l name), but it
## is NOT the shipped fail-closed lib and its hash is NOT recorded — the default pin
## libphi_crypto.a.sha256 is deliberately left untouched (never re-pin it here). Restore the default
## afterwards with `make phicrypto-lib && make phicrypto-lib-verify`. Prerequisite for the
## `voting_snark` cgo build/test. Uses --locked --offline (vendored, pinned deps), path-remapped for a
## stable artifact — same discipline as phicrypto-lib.
phicrypto-lib-snark:
	@echo ">> building phi-crypto voting_snark-featured C-ABI superset staticlib from $(PHICRYPTO_DIR)"
	@cd "$(PHICRYPTO_DIR)" && CARGO_ENCODED_RUSTFLAGS="--remap-path-prefix=$$(pwd)=." cargo build --release --locked --offline --features voting_snark
	@cp "$(PHICRYPTO_DIR)/target/release/libphi_crypto.a" phicrypto/lib/
	@cp "$(PHICRYPTO_DIR)/phi_crypto.h" phicrypto/lib/
	@cp "$(PHICRYPTO_DIR)/phi_crypto_voting_snark.h" phicrypto/lib/
	@echo ">> phicrypto/lib/ staged with the voting_snark superset staticlib + phi_crypto.h + phi_crypto_voting_snark.h"
	@echo ">> NOTE: default libphi_crypto.a.sha256 is unchanged; run 'make phicrypto-lib' to restore the shipped lib"

## build-cgo-snark: build phid with the derivation-proof verify path linked (run 'make phicrypto-lib-snark' first).
build-cgo-snark:
	@echo ">> building $(BINARY) with phicrypto_cgo + voting_snark (derivation-proof tally path)"
	@go build -tags 'pebbledb phicrypto_cgo voting_snark' -ldflags '$(ldflags)' -o "$(BUILDDIR)/$(BINARY)" ./cmd/phid

## test-cgo-snark: run the voting suite with the derivation-proof path linked (run 'make phicrypto-lib-snark' first).
test-cgo-snark:
	@go test -tags 'pebbledb phicrypto_cgo voting_snark' ./x/voting/... ./phicrypto/...

## vet: static analysis.
vet:
	@go vet ./...

## proto-gen: generate Go code from proto (requires buf + protoc-gen-gocosmos + protoc-gen-grpc-gateway in PATH).
proto-gen:
	@bash ./scripts/protocgen.sh

## tidy: tidy go.mod.
tidy:
	@go mod tidy

## clean: remove build outputs.
clean:
	@rm -rf $(BUILDDIR)
