#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Supply-chain integrity gate for the phi-crypto C-ABI staticlib.
#
# The chain links libphi_crypto.a only under -tags phicrypto_cgo. That artifact must be
# reproducible from the pinned phi-crypto source and must never be an opaque committed blob.
# This script recomputes the SHA-256 of the freshly built staticlib and compares it against
# the checked-in pin. CI runs it right before the cgo link; `make phicrypto-lib-verify` runs
# it locally after `make phicrypto-lib`.
#
# Usage: verify-phicrypto-lib.sh [<staticlib> [<pin-file>]]
set -euo pipefail

LIB="${1:-phicrypto/lib/libphi_crypto.a}"
PIN="${2:-phicrypto/lib/libphi_crypto.a.sha256}"
PLACEHOLDER="UNINITIALIZED"

if [ ! -f "$LIB" ]; then
  echo "error: staticlib not found: $LIB (run 'make phicrypto-lib' first)" >&2
  exit 1
fi
if [ ! -f "$PIN" ]; then
  echo "error: pin file not found: $PIN" >&2
  exit 1
fi

# Portable SHA-256: sha256sum on Linux/CI, shasum -a 256 on macOS.
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$LIB" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "$LIB" | awk '{print $1}')"
fi

# First non-comment, non-empty token is the pinned hash.
expected="$(grep -v '^[[:space:]]*#' "$PIN" | awk 'NF{print $1; exit}')"

if [ -z "$expected" ] || [ "$expected" = "$PLACEHOLDER" ]; then
  {
    echo "error: hash pin is not initialized ($PIN)."
    echo "       computed SHA-256 of $LIB:"
    echo "         $actual"
    echo "       Build the staticlib in the canonical environment (Linux x86_64, the Rust"
    echo "       toolchain pinned in .github/workflows/ci.yml) and run 'make phicrypto-lib-pin'"
    echo "       to record it, then commit $PIN."
  } >&2
  exit 1
fi

if [ "$actual" != "$expected" ]; then
  {
    echo "error: phi-crypto staticlib hash mismatch — supply-chain gate."
    echo "       expected (pinned): $expected"
    echo "       actual   (built):  $actual"
    echo "       The linked C-ABI blob does NOT match the pinned phi-crypto source."
    echo "       Rebuild from the pinned source, or — if the change is intended — review it and"
    echo "       re-pin with 'make phicrypto-lib-pin'."
  } >&2
  exit 1
fi

echo "ok: phi-crypto staticlib matches the pinned SHA-256 ($actual)"
