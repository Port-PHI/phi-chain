// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/internal/storeprefix"
	"github.com/Port-PHI/phi-chain/internal/storeprefix/prefixtest"

	insttypes "github.com/Port-PHI/phi-chain/x/institutions/types"
)

func paramsPrefix(t *testing.T, m phiModule) []byte {
	t.Helper()
	for _, p := range m.declared {
		if p.Name == "params" {
			return p.Bytes
		}
	}
	t.Fatalf("module %q declares no params prefix", m.name)
	return nil
}

// TestNet_ParamsAreNonDefaultForEveryModule is the non-vacuity guard: after the fixture seeds, EVERY module's stored params must differ from a chain still on defaults.
func TestNet_ParamsAreNonDefaultForEveryModule(t *testing.T) {
	gv := newGenesisValidator(t)

	ref := startChain(t, seedRichGenesis(t, baseGenesis(t, gv), gv)).dumpAll()

	first := startChain(t, seedRichGenesis(t, baseGenesis(t, gv), gv))
	seedEveryKeyspace(t, first, gv)
	before := first.dumpAll()

	for _, m := range phiModules() {
		pfx := paramsPrefix(t, m)
		refParams := storeprefix.Under(ref[m.name], pfx)
		gotParams := storeprefix.Under(before[m.name], pfx)

		require.NotEmpty(t, gotParams, "module %q has no params record to round-trip", m.name)
		require.NotEqual(t, refParams, gotParams,
			"module %q params were not moved off their defaults — a dropped params keyspace would "+
				"round-trip vacuously for it", m.name)
	}
}

// TestNet_DroppedParamsIsCaughtForEveryModule proves the consequence: because the seeded params are non-default, a params record lost on export (absent on the second chain) OR reset to defaults on import (a re-init from a params-less genesis) is a mismatch the net reports — for every module.
func TestNet_DroppedParamsIsCaughtForEveryModule(t *testing.T) {
	gv := newGenesisValidator(t)

	ref := startChain(t, seedRichGenesis(t, baseGenesis(t, gv), gv)).dumpAll()
	first := startChain(t, seedRichGenesis(t, baseGenesis(t, gv), gv))
	seedEveryKeyspace(t, first, gv)
	before := first.dumpAll()

	for _, m := range phiModules() {
		observable := keepObservable(m.name, m.declared)
		pfx := paramsPrefix(t, m)

		afterDropped := cloneWithout(before[m.name], pfx)
		require.NotEmpty(t, prefixtest.RoundTripProblems(observable, before[m.name], afterDropped),
			"module %q: a params record dropped on export must be caught", m.name)

		afterDefaulted := cloneReplacingPrefix(before[m.name], pfx, storeprefix.Under(ref[m.name], pfx))
		require.NotEmpty(t, prefixtest.RoundTripProblems(observable, before[m.name], afterDefaulted),
			"module %q: params reset to defaults on import must be caught", m.name)

		refDump := ref[m.name]
		require.Empty(t, prefixtest.RoundTripProblems(observable, refDump, refDump),
			"module %q: default-vs-default is the vacuous state the non-default seed removes", m.name)
	}
}

// TestNet_PerDidRedeemFloorCapRoundTrips covers the per-DID daily redeem cap end to end: the non-default, above-floor value the fixture seeds is exported and re-imported unchanged, and the genesis import path REFUSES a sub-floor value — the floor is a property of the import, not only of the message handler.
func TestNet_PerDidRedeemFloorCapRoundTrips(t *testing.T) {
	gv := newGenesisValidator(t)
	first := startChain(t, seedRichGenesis(t, baseGenesis(t, gv), gv))
	seedEveryKeyspace(t, first, gv)
	first.advanceBlock(t)

	require.Equal(t, nonDefaultPerDidCapUphi,
		first.app.InstitutionsKeeper.GetParams(first.ctx()).RedeemDailyCapPerDidUphi,
		"precondition: the non-default per-DID cap is in force")

	second := startChain(t, first.export(t))
	require.Equal(t, nonDefaultPerDidCapUphi,
		second.app.InstitutionsKeeper.GetParams(second.ctx()).RedeemDailyCapPerDidUphi,
		"the per-DID redeem cap must survive export→import")

	gs := insttypes.DefaultGenesis()
	gs.Params.RedeemDailyCapPerDidUphi = "99999" // a tenth of a PHI in uphi, ten times beneath the floor
	require.Error(t, gs.Validate(), "genesis must refuse a sub-floor per-DID redeem cap")

	gs.Params.RedeemDailyCapPerDidUphi = nonDefaultPerDidCapUphi
	require.NoError(t, gs.Validate(), "an above-floor per-DID cap is accepted")
}

func cloneWithout(dump map[string]string, prefix []byte) map[string]string {
	out := map[string]string{}
	for k, v := range dump {
		if len(k) >= len(prefix) && k[:len(prefix)] == string(prefix) {
			continue
		}
		out[k] = v
	}
	return out
}

func cloneReplacingPrefix(dump map[string]string, prefix []byte, replacement map[string]string) map[string]string {
	out := cloneWithout(dump, prefix)
	for k, v := range replacement {
		out[k] = v
	}
	return out
}
