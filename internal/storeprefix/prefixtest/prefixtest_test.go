// SPDX-License-Identifier: Apache-2.0

package prefixtest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/internal/storeprefix"
)

var derivedPrefix = []byte{0xAA}

func rec(suffix, val string) (string, string) {
	return string(append(append([]byte{}, derivedPrefix...), suffix...)), val
}

func derivedDump(n int) map[string]string {
	out := map[string]string{}
	for _, s := range []string{"a", "b", "c"}[:n] {
		k, v := rec(s, "v-"+s)
		out[k] = v
	}
	return out
}

func derivedDecl() []storeprefix.Prefix {
	return []storeprefix.Prefix{{Name: "derived_index", Bytes: derivedPrefix, Carry: storeprefix.CarryDerived, Reason: "rebuilt on import"}}
}

// A correct rebuild — before and after identical — reports no problem.
func TestCarryDerived_CorrectRebuildPasses(t *testing.T) {
	before, after := derivedDump(3), derivedDump(3)
	require.Empty(t, RoundTripProblems(derivedDecl(), before, after))
}

// A SUBSET lost on import (two of three records) fails, where NotEmpty would have passed.
func TestCarryDerived_PartialLossFails(t *testing.T) {
	before, after := derivedDump(3), derivedDump(2)
	require.NotEmpty(t, after, "precondition: the old NotEmpty check would have passed this")

	problems := RoundTripProblems(derivedDecl(), before, after)
	require.NotEmpty(t, problems, "a partial loss of a derived index must be caught")
	require.Contains(t, problems[0], "from-scratch recompute")
}

// A record present but with the WRONG value is caught too — deep, not just cardinality.
func TestCarryDerived_WrongValueFails(t *testing.T) {
	before := derivedDump(3)
	after := derivedDump(3)
	for k := range after {
		after[k] = "corrupted"
		break
	}
	require.NotEmpty(t, RoundTripProblems(derivedDecl(), before, after))
}

// A derived index that vanished entirely is reported as empty, not as a mismatch — a distinct cause.
func TestCarryDerived_EmptyIsReportedDistinctly(t *testing.T) {
	problems := RoundTripProblems(derivedDecl(), derivedDump(3), map[string]string{})
	require.Len(t, problems, 1)
	require.Contains(t, problems[0], "came back empty")
}

// The other carry modes still behave: CarryExact demands identity, CarryDropped demands emptiness.
func TestCarry_ExactAndDropped(t *testing.T) {
	exact := []storeprefix.Prefix{{Name: "x", Bytes: derivedPrefix, Carry: storeprefix.CarryExact}}
	require.Empty(t, RoundTripProblems(exact, derivedDump(3), derivedDump(3)))
	require.NotEmpty(t, RoundTripProblems(exact, derivedDump(3), derivedDump(2)))

	dropped := []storeprefix.Prefix{{Name: "d", Bytes: derivedPrefix, Carry: storeprefix.CarryDropped, Reason: "not carried"}}
	require.Empty(t, RoundTripProblems(dropped, derivedDump(3), map[string]string{}))
	require.NotEmpty(t, RoundTripProblems(dropped, derivedDump(3), derivedDump(1)))
}
