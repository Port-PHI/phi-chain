// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"os"
	"testing"

	"cosmossdk.io/log"
	dbm "github.com/cosmos/cosmos-db"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/app"
)

// TestMain configures the "phi" bech32 prefix that NewApp relies on (production does this in the
// node's PreRun via cmd/config.go).
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

// Module invariants must actually be registered with x/crisis. In SDK v0.53
// module.Manager.RegisterInvariants is a no-op, so the app wires them by iterating HasInvariants
// modules. The institutions solvency invariants must therefore appear as crisis routes so the
// EndBlock sweep and MsgVerifyInvariant can enforce them.
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
	// Mint-within-backing / backing-shortfall are intentionally NOT registered — a legitimate
	// LOW_LIQ attestation must never halt the chain.
	require.False(t, got["institutions/mint-within-backing"], "mint-within-backing must not be a crisis route")
	require.False(t, got["institutions/backing-shortfall"], "backing-shortfall must not be a crisis route")
}

// Governance deposit handling must be supply-neutral. The bond/gov denom is the vault-backed
// uphi, so burning a deposit would shrink supply and break solvency. The gov genesis therefore does
// not burn vetoed/failed deposits and routes the cancellation share to an account.
func TestGovGenesisIsSupplyNeutral(t *testing.T) {
	a := newTestApp(t)
	var govGen govv1.GenesisState
	require.NoError(t, a.AppCodec().UnmarshalJSON(a.DefaultGenesis()[govtypes.ModuleName], &govGen))
	require.False(t, govGen.Params.BurnVoteVeto, "vetoed deposits must be refunded, not burned (supply-neutral)")
	require.False(t, govGen.Params.BurnProposalDepositPrevote, "failed-prevote deposits must not be burned")
	require.NotEmpty(t, govGen.Params.ProposalCancelDest, "cancellation deposits must be routed to an account, not burned")
}
