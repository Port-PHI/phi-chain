// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"testing"

	"cosmossdk.io/math"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
)

// A deposit-burn flag armed at proposal execution must not destroy vault-backed uphi (drives the real gov keeper).
func TestGovDepositBurn_NeutralizedWhileVaultsBacked(t *testing.T) {
	c := setupProtocolFee(t, 20)

	govParams := govv1.DefaultParams()
	govParams.BurnVoteVeto = true
	require.NoError(t, c.app.GovKeeper.Params.Set(c.ctx, govParams))

	c.mint(t, "1000000", "dep-1")
	c.requireSolvent(t)
	require.True(t, c.vault(t).IsPositive(), "the institution vault must be non-zero for this scenario")
	supplyBefore := c.supply()

	const propID = uint64(1)
	depositUphi := math.NewInt(500_000)
	require.NoError(t, c.app.BankKeeper.SendCoinsFromAccountToModule(c.ctx, c.holder, govtypes.ModuleName, cointypes.CoinsOf(depositUphi)))
	require.NoError(t, c.app.GovKeeper.SetDeposit(c.ctx, govv1.Deposit{
		ProposalId: propID, Depositor: c.holder.String(), Amount: cointypes.CoinsOf(depositUphi),
	}))

	feeCollector := c.app.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName)
	feeBefore := c.app.BankKeeper.GetBalance(c.ctx, feeCollector, cointypes.Denom).Amount

	require.NoError(t, c.app.GovKeeper.DeleteAndBurnDeposits(c.ctx, propID))

	require.Equal(t, supplyBefore, c.supply(),
		"a gov deposit burn must not shrink supply while institution vaults back uphi")
	feeAfter := c.app.BankKeeper.GetBalance(c.ctx, feeCollector, cointypes.Denom).Amount
	require.Equal(t, depositUphi, feeAfter.Sub(feeBefore),
		"the forfeited deposit is redirected to the fee collector, not destroyed")

	c.requireSolvent(t)
}
