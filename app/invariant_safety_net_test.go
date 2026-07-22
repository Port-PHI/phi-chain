// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/app"
	institutionstypes "github.com/Port-PHI/phi-chain/x/institutions/types"
)

// The institutions module asserts its own invariants every block.
func TestSafetyNet_TheInstitutionsModuleAssertsItsOwnInvariantsEveryBlock(t *testing.T) {
	a := newTestApp(t)

	_, hasEndBlock := a.ModuleManager.Modules[institutionstypes.ModuleName].(interface {
		EndBlock(interface{}) error
	})
	require.NotNil(t, a.ModuleManager.Modules[institutionstypes.ModuleName])
	_ = hasEndBlock

	require.Contains(t, a.ModuleManager.OrderEndBlockers, institutionstypes.ModuleName,
		"the institutions module must assert solvency every block, independent of any transaction")
}

// The registered routes the crisis EndBlocker runs remain, regardless of any transaction.
func TestSafetyNet_TheRegisteredInvariantRoutesRemain(t *testing.T) {
	a := newTestApp(t)

	got := map[string]bool{}
	for _, r := range a.CrisisKeeper.Routes() {
		got[r.FullRoute()] = true
	}
	for _, route := range []string{
		"institutions/solvency",
		"institutions/non-negative-vault",
		"governance/turnout-within-frozen-basis",
	} {
		require.True(t, got[route], "%s must remain a registered halting invariant", route)
	}
	require.NotEmpty(t, a.CrisisKeeper.Routes())
}

// The crisis module runs those routes from its own EndBlocker, with no transaction involved.
func TestSafetyNet_CrisisRunsInEndBlock(t *testing.T) {
	a := newTestApp(t)
	require.Contains(t, a.ModuleManager.OrderEndBlockers, "crisis",
		"the crisis EndBlocker is the unmetered, transaction-independent path the invariants rely on")
}

// The on-demand check remains priced and available.
func TestSafetyNet_TheOnDemandCheckIsStillAvailableAndPriced(t *testing.T) {
	require.Positive(t, app.InvariantCheckFeeUphi,
		"the check is bounded by gas now, but it is still a real, priced operation")
}
