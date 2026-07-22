// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

// A deposit marker keyed under another prefix must abort InitGenesis rather than reach the store.
func TestInitGenesisRejectsAStoreEntryUnderAForeignPrefix(t *testing.T) {
	f := setup(t)

	gs := types.DefaultGenesis()
	gs.Params = f.k.GetParams(f.ctx)
	gs.DepositMarkers = []types.StoreEntry{{
		Key:   append(append([]byte(nil), types.InstitutionPrefix...), []byte("bank-of-nowhere")...),
		Value: []byte{types.DepositMarkerByte},
	}}

	require.Panics(t, func() { f.k.InitGenesis(f.ctx, *gs) },
		"a store entry outside its declared prefix must abort genesis")

	_, found := f.k.GetInstitution(f.ctx, "bank-of-nowhere")
	require.False(t, found, "the forged institution record must not exist")
}

// The same guard applies to the other two raw-marker fields.
func TestInitGenesisRejectsForeignPrefixesInEveryRawMarkerField(t *testing.T) {
	foreign := func(prefix []byte) []types.StoreEntry {
		return []types.StoreEntry{{
			Key:   append(append([]byte(nil), prefix...), []byte("x")...),
			Value: []byte{types.DepositMarkerByte},
		}}
	}

	for _, tc := range []struct {
		name  string
		apply func(gs *types.GenesisState)
	}{
		{"cap_counters", func(gs *types.GenesisState) { gs.CapCounters = foreign(types.RolePrefix) }},
		{"approvals", func(gs *types.GenesisState) { gs.Approvals = foreign(types.FxRequestPrefix) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			gs := types.DefaultGenesis()
			gs.Params = f.k.GetParams(f.ctx)
			tc.apply(gs)
			require.Panics(t, func() { f.k.InitGenesis(f.ctx, *gs) })
		})
	}
}

// A genesis whose markers are correctly keyed imports and round-trips unchanged.
func TestInitGenesisImportsWellFormedStoreEntriesAndRoundTrips(t *testing.T) {
	f := setup(t)

	depositKey := append(append([]byte(nil), types.DepositPrefix...), []byte("inst-1|mint|ref-7")...)
	approvalKey := append(append([]byte(nil), types.ApprovalPrefix...), []byte("inst-1|hash|addr")...)
	counterKey := append(append([]byte(nil), types.CounterPrefix...), []byte("inst-1|mint|20260719")...)

	gs := types.DefaultGenesis()
	gs.Params = f.k.GetParams(f.ctx)
	gs.DepositMarkers = []types.StoreEntry{{Key: depositKey, Value: []byte{types.DepositMarkerByte}}}
	gs.Approvals = []types.StoreEntry{{Key: approvalKey, Value: make([]byte, 8)}}
	gs.CapCounters = []types.StoreEntry{{Key: counterKey, Value: []byte("500")}}

	require.NotPanics(t, func() { f.k.InitGenesis(f.ctx, *gs) })

	out := f.k.ExportGenesis(f.ctx)
	require.Equal(t, gs.DepositMarkers, out.DepositMarkers)
	require.Equal(t, gs.Approvals, out.Approvals)
	require.Equal(t, gs.CapCounters, out.CapCounters)
}
