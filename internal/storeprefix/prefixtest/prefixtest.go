// SPDX-License-Identifier: Apache-2.0

// Package prefixtest asserts every prefix in a module's declaration does what it claims across an export→import cycle.
package prefixtest

import (
	"fmt"
	"testing"

	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/internal/storeprefix"
)

// Dump reads a module's whole keyspace into a comparable map.
func Dump(ctx sdk.Context, key storetypes.StoreKey) map[string]string {
	out := map[string]string{}
	it := ctx.KVStore(key).Iterator(nil, nil)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		out[string(it.Key())] = string(it.Value())
	}
	return out
}

// RequireSeeded asserts every declared prefix carries a record BEFORE the round-trip, so an empty-both-sides keyspace cannot pass vacuously.
func RequireSeeded(t *testing.T, before map[string]string, declared []storeprefix.Prefix) {
	t.Helper()
	for _, p := range declared {
		require.NotEmpty(t, storeprefix.Under(before, p.Bytes),
			"prefix %q was never seeded, so the round-trip below would prove nothing about it", p.Name)
	}
}

// RequireRoundTrip asserts each declared prefix behaved as its Carry says and nothing outside the declaration appeared or vanished.
func RequireRoundTrip(t *testing.T, declared []storeprefix.Prefix, before, after map[string]string) {
	t.Helper()
	for _, pr := range RoundTripProblems(declared, before, after) {
		require.Fail(t, "genesis round-trip", pr)
	}
}

// RoundTripProblems returns a problem for every declared prefix that did not behave as its Carry says, plus any record outside the declaration; empty means the round-trip held.
func RoundTripProblems(declared []storeprefix.Prefix, before, after map[string]string) []string {
	var problems []string
	for _, p := range declared {
		want, got := storeprefix.Under(before, p.Bytes), storeprefix.Under(after, p.Bytes)
		switch p.Carry {
		case storeprefix.CarryExact:
			if !equalRecords(want, got) {
				problems = append(problems, fmt.Sprintf("prefix %q did not survive export→import", p.Name))
			}
		case storeprefix.CarryDerived:
			// Non-emptiness first, so a vanished keyspace reports distinctly from a mismatch.
			if len(got) == 0 {
				problems = append(problems, fmt.Sprintf(
					"prefix %q is declared as rebuilt on import (%s) but came back empty", p.Name, p.Reason))
				continue
			}
			// Then EVERY record: deep-compare against the from-scratch recompute catches a partial loss NotEmpty would pass.
			if !equalRecords(want, got) {
				problems = append(problems, fmt.Sprintf(
					"prefix %q is rebuilt on import (%s), but the rebuilt index does not match a from-scratch "+
						"recompute over the carried base state — a partial loss NotEmpty could not see", p.Name, p.Reason))
			}
		case storeprefix.CarryDropped:
			if len(got) != 0 {
				problems = append(problems, fmt.Sprintf(
					"prefix %q is declared as not carried (%s) but came back populated", p.Name, p.Reason))
			}
		}
	}

	// Nothing may live outside the declaration on either side.
	problems = append(problems, undeclaredProblems("before", before, declared)...)
	problems = append(problems, undeclaredProblems("after", after, declared)...)
	return problems
}

func equalRecords(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

func undeclaredProblems(side string, dump map[string]string, declared []storeprefix.Prefix) []string {
	var out []string
	for k := range dump {
		matched := false
		for _, p := range declared {
			if len(k) >= len(p.Bytes) && k[:len(p.Bytes)] == string(p.Bytes) {
				matched = true
				break
			}
		}
		if !matched {
			out = append(out, fmt.Sprintf("%s: key %X lies under no declared store prefix", side, []byte(k)))
		}
	}
	return out
}
