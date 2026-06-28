//go:build !phicrypto_cgo

// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"testing"

	"cosmossdk.io/log"
	dbm "github.com/cosmos/cosmos-db"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/app"
)

// In the default (tagless) build the phi-crypto verifier is the fail-closed Disabled stub
// (DefaultEnforces() == false). A consensus node built this way must refuse to start at ANY height —
// including genesis (height 0) — or it would reject every crypto-dependent message and fork from
// cgo-built validators. Enforcement is keyed on the explicit enforceCrypto flag, not on
// block height, so offline/export/gentx/test paths (enforceCrypto=false) stay usable. These tests are
// meaningful only without -tags phicrypto_cgo (the cgo build enforces unconditionally).

// A node-path construction (enforceCrypto=true) must panic at genesis with the Disabled verifier.
func TestNewApp_NodeRefusesDisabledCryptoAtGenesis(t *testing.T) {
	require.Panics(t, func() {
		_ = app.NewApp(log.NewNopLogger(), dbm.NewMemDB(), nil, true,
			simtestutil.NewAppOptionsWithFlagHome(t.TempDir()), true /* enforceCrypto: consensus node */)
	}, "a node with the Disabled verifier must refuse to start, even at genesis (height 0)")
}

// An offline/export/test construction (enforceCrypto=false) must remain usable with the default build.
func TestNewApp_NonNodePathAllowedWithDisabledCrypto(t *testing.T) {
	require.NotPanics(t, func() {
		_ = app.NewApp(log.NewNopLogger(), dbm.NewMemDB(), nil, true,
			simtestutil.NewAppOptionsWithFlagHome(t.TempDir()), false /* enforceCrypto: offline/export/test */)
	}, "offline/export/test construction must stay usable without -tags phicrypto_cgo")
}
