// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"os"
	"testing"

	"cosmossdk.io/log"
	dbm "github.com/cosmos/cosmos-db"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/app"
	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	identitytypes "github.com/Port-PHI/phi-chain/x/identity/types"
	institutionstypes "github.com/Port-PHI/phi-chain/x/institutions/types"
)

// TestMain configures the "phi" bech32 prefix that NewApp relies on.
func TestMain(m *testing.M) {
	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount(app.AccountAddressPrefix, app.AccountAddressPrefix+"pub")
	cfg.SetBech32PrefixForValidator(app.AccountAddressPrefix+"valoper", app.AccountAddressPrefix+"valoperpub")
	cfg.SetBech32PrefixForConsensusNode(app.AccountAddressPrefix+"valcons", app.AccountAddressPrefix+"valconspub")
	os.Exit(m.Run())
}

func newTestApp(t *testing.T) *app.App {
	t.Helper()
	return app.NewApp(log.NewNopLogger(), dbm.NewMemDB(), nil, true, simtestutil.NewAppOptionsWithFlagHome(t.TempDir()), false)
}

// The institutions solvency invariants must be registered as crisis routes.
func TestCrisisInvariantsRegistered(t *testing.T) {
	a := newTestApp(t)
	routes := a.CrisisKeeper.Routes()
	require.NotEmpty(t, routes, "x/crisis must have registered invariant routes (the no-op was replaced)")

	got := map[string]bool{}
	for _, r := range routes {
		got[r.FullRoute()] = true
	}
	require.True(t, got["institutions/solvency"], "solvency must be a registered halting invariant")
	require.True(t, got["institutions/non-negative-vault"], "non-negative-vault must be a registered halting invariant")
	require.True(t, got["governance/turnout-within-frozen-basis"],
		"the turnout-within-frozen-denominator invariant must be a registered halting invariant")
	require.False(t, got["institutions/mint-within-backing"], "mint-within-backing must not be a crisis route")
	require.False(t, got["institutions/backing-shortfall"], "backing-shortfall must not be a crisis route")
}

// The gov-burn guard unwraps MsgSubmitProposal but not authz MsgExec, sound only while x/authz is absent.
func TestAuthzModuleIsNotRegistered(t *testing.T) {
	a := newTestApp(t)
	_, ok := a.ModuleManager.Modules["authz"]
	require.False(t, ok,
		"x/authz must not be registered; if it is added, RejectUnsafeGovParamsDecorator must unwrap MsgExec")
}

// The burn-capable module set must be EXACTLY the neutralised set; any new Burner grant breaks solvency and halts.
func TestBurnCapableModuleAccountsAreExactlyTheNeutralisedSet(t *testing.T) {
	neutralised := []string{
		stakingtypes.BondedPoolName,
		stakingtypes.NotBondedPoolName,
		govtypes.ModuleName,
		institutionstypes.ModuleName,
	}

	a := newTestApp(t)
	var wired []string
	for name, perms := range a.AccountKeeper.GetModulePermissions() {
		if perms.HasPermission(authtypes.Burner) {
			wired = append(wired, name)
		}
	}
	require.ElementsMatch(t, neutralised, wired,
		"the set of burn-capable module accounts changed; a burn source that is not neutralised will "+
			"break the solvency invariant and halt the chain in the institutions EndBlock")

	var declared []string
	for name, perms := range app.GetMaccPerms() {
		for _, p := range perms {
			if p == authtypes.Burner {
				declared = append(declared, name)
				break
			}
		}
	}
	require.ElementsMatch(t, neutralised, declared,
		"declared Burner permissions disagree with the neutralised set")

	for _, name := range []string{identitytypes.ModuleName, cointypes.RevenueAccountName} {
		perms, ok := a.AccountKeeper.GetModulePermissions()[name]
		require.True(t, ok, "%s must be a registered module account", name)
		require.False(t, perms.HasPermission(authtypes.Burner), "%s must never hold Burner", name)
		require.False(t, perms.HasPermission(authtypes.Minter), "%s must never hold Minter", name)
	}
}

// Gov deposit handling must be supply-neutral: uphi is vault-backed, so burning a deposit breaks solvency.
func TestGovGenesisIsSupplyNeutral(t *testing.T) {
	a := newTestApp(t)
	var govGen govv1.GenesisState
	require.NoError(t, a.AppCodec().UnmarshalJSON(a.DefaultGenesis()[govtypes.ModuleName], &govGen))
	require.False(t, govGen.Params.BurnVoteVeto, "vetoed deposits must be refunded, not burned (supply-neutral)")
	require.False(t, govGen.Params.BurnProposalDepositPrevote, "failed-prevote deposits must not be burned")
	require.False(t, govGen.Params.BurnVoteQuorum, "quorum-failed deposits must not be burned (pinned false so an SDK default flip cannot break solvency)")
	require.NotEmpty(t, govGen.Params.ProposalCancelDest, "cancellation deposits must be routed to an account, not burned")
}
