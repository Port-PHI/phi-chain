#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Hard gate over govulncheck JSON output.

govulncheck has no native ignore mechanism, so this script enforces the policy
directly: the build FAILS on any symbol-reachable vulnerability EXCEPT a small,
explicitly documented allowlist of advisories that have no upstream fix for the
dependency versions this project is pinned to. Every other finding -- including
any newly disclosed one, and any standard-library regression -- fails the gate.
Import-only ("not called") findings are reported but do not fail the build,
matching govulncheck's own "affected" semantics.

The script fails closed: any unreadable file or unparseable tool output exits
non-zero rather than passing.

Usage: govulncheck_gate.py <govulncheck-json-file>
"""

import json
import sys

# Advisories with NO upstream fix for the versions pinned in go.mod. Each is
# symbol-reachable but cannot be remediated without leaving the Cosmos SDK
# framework; each is documented individually below with its rationale.
#
#   GO-2024-2584  cosmos-sdk slashing evasion -- fixed only on the 0.47.x line
#                 (0.47.10); no fix is published for the 0.50+/0.53 line in use.
#   GO-2023-1881  cosmos-sdk x/crisis does not charge ConstantFee -- WONTFIX
#                 upstream (x/crisis is deprecated and slated for replacement).
#   GO-2023-1821  cosmos-sdk x/crisis does not halt the chain -- WONTFIX
#                 upstream (same deprecated module).
#   GO-2026-5932  golang.org/x/crypto/openpgp/armor -- reached only through cosmos-sdk's keyring key
#                 armoring (client-side `keys` import/export, file backend), never the consensus path;
#                 Phi uses no OpenPGP and decodes no untrusted armored input. x/crypto/openpgp is
#                 deprecated/frozen with no fixed version for the pinned x/crypto line.
ALLOWLIST = {
    "GO-2024-2584",
    "GO-2023-1881",
    "GO-2023-1821",
    "GO-2026-5932",
}


def reachable_advisories(stream):
    """Return {osv_id: "pkg.Func"} for every symbol-reachable finding.

    govulncheck emits a sequence of single-key JSON objects (it pretty-prints
    each message, so the stream is not line-delimited). A finding is treated as
    symbol-reachable when any frame in its trace names a function; import-only
    findings carry module/package frames with no function and are skipped.
    """
    reachable = {}
    decoder = json.JSONDecoder()
    idx, length = 0, len(stream)
    while idx < length:
        while idx < length and stream[idx] in " \t\r\n":
            idx += 1
        if idx >= length:
            break
        obj, idx = decoder.raw_decode(stream, idx)
        if not isinstance(obj, dict):
            continue
        finding = obj.get("finding")
        if not isinstance(finding, dict):
            continue
        osv = finding.get("osv")
        trace = finding.get("trace") or []
        called = [f for f in trace if isinstance(f, dict) and f.get("function")]
        if osv and called:
            frame = called[0]
            reachable[osv] = "%s.%s" % (
                frame.get("package", "?"),
                frame.get("function", "?"),
            )
    return reachable


def main(path):
    try:
        with open(path, "r", encoding="utf-8") as handle:
            raw = handle.read()
    except OSError as err:
        print("govulncheck-gate: cannot read %s: %s" % (path, err), file=sys.stderr)
        return 2

    try:
        reachable = reachable_advisories(raw)
    except ValueError as err:
        # Fail closed: never pass on output we could not fully parse.
        print("govulncheck-gate: unparseable govulncheck JSON: %s" % err, file=sys.stderr)
        return 2

    print("govulncheck-gate: symbol-reachable advisories: %s"
          % (", ".join(sorted(reachable)) or "(none)"))

    allowed = sorted(set(reachable) & ALLOWLIST)
    if allowed:
        print("govulncheck-gate: allowed (documented, no upstream fix): %s"
              % ", ".join(allowed))

    unexpected = sorted(set(reachable) - ALLOWLIST)
    if unexpected:
        print("govulncheck-gate: FAIL -- un-allowlisted reachable advisories:",
              file=sys.stderr)
        for osv in unexpected:
            print("  %s  via %s" % (osv, reachable[osv]), file=sys.stderr)
        print("Fix by bumping to the patched dependency version. Only if no "
              "upstream fix exists, add the ID to ALLOWLIST with a written "
              "rationale.", file=sys.stderr)
        return 1

    print("govulncheck-gate: PASS -- no un-allowlisted reachable advisories.")
    return 0


if __name__ == "__main__":
    if len(sys.argv) != 2:
        print("usage: govulncheck_gate.py <govulncheck-json-file>", file=sys.stderr)
        sys.exit(2)
    sys.exit(main(sys.argv[1]))
