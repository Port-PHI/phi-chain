# syntax=docker/dockerfile:1
# SPDX-License-Identifier: Apache-2.0
#
# Build the phid node with the REAL phi-crypto verifier linked (-tags phicrypto_cgo).
# Both halves build strictly from each repo's committed, vendored dependencies — no
# project dependency is fetched from the network at build time.
#
# Build context expects the phi-crypto source under ./phi-crypto-src (the release
# workflow checks it out there).

# 1) phi-crypto C-ABI staticlib (Rust, offline from its vendored crates).
FROM rust:1-bookworm AS crypto
WORKDIR /crypto
COPY phi-crypto-src/ ./
RUN cargo build --release --locked --offline

# 2) phid (Go, offline-vendored, cgo + pebbledb, real verifier linked).
FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY . .
COPY --from=crypto /crypto/target/release/libphi_crypto.a phicrypto/lib/
COPY --from=crypto /crypto/phi_crypto.h phicrypto/lib/
ENV CGO_ENABLED=1 GOTOOLCHAIN=local GOFLAGS=-mod=vendor GOPROXY=off GOSUMDB=off
RUN go build -trimpath -tags 'pebbledb phicrypto_cgo' -o /out/phid ./cmd/phid

# 3) Minimal runtime image.
FROM debian:bookworm-slim AS runtime
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --create-home --uid 1000 phi
COPY --from=build /out/phid /usr/local/bin/phid
USER phi
WORKDIR /home/phi
# RPC/WS · gRPC · REST/LCD · P2P
EXPOSE 26657 9090 1317 26656
ENTRYPOINT ["phid"]
CMD ["start"]
