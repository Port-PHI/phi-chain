// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"encoding/json"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	crisistypes "github.com/cosmos/cosmos-sdk/x/crisis/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/app"
	phiante "github.com/Port-PHI/phi-chain/app/ante"
	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
)

func blockFillCostUphi() int64 {
	txsPerBlock := app.DefaultBlockMaxGas / int64(phiante.MaxTxGas)
	fee, _ := math.NewIntFromString(cointypes.DefaultTransferFee)
	return txsPerBlock * fee.Int64()
}

// The ceiling must sit well above the heaviest ordinary handler and well below the block limit.
func TestSpamPricing_PerTxGasCeilingIsBoundedButGenerous(t *testing.T) {
	const heaviestMeasuredHandlerGas = 52_425 // recovery execution, the largest measured

	require.Less(t, phiante.MaxTxGas, uint64(app.DefaultBlockMaxGas),
		"one transaction must never be able to consume a whole block")
	require.GreaterOrEqual(t, phiante.MaxTxGas, uint64(heaviestMeasuredHandlerGas)*20,
		"the ceiling must leave a wide margin over the heaviest ordinary handler")

	require.GreaterOrEqual(t, app.DefaultBlockMaxGas/int64(phiante.MaxTxGas), int64(50),
		"a block must hold enough transactions that filling it is not cheap")
}

// Filling a block must cost materially more than under the previous ten-million ceiling.
func TestSpamPricing_FillingABlockCostsMateriallyMore(t *testing.T) {
	const previousMaxTxGas = int64(10_000_000)

	fee, ok := math.NewIntFromString(cointypes.DefaultTransferFee)
	require.True(t, ok)
	previousCost := (app.DefaultBlockMaxGas / previousMaxTxGas) * fee.Int64()
	currentCost := blockFillCostUphi()

	t.Logf("cost to fill one block: was %d uphi, now %d uphi", previousCost, currentCost)
	require.GreaterOrEqual(t, currentCost, previousCost*5,
		"the cost of filling a block must have risen by at least five times")
}

// The permissionless invariant check must cost far more than an ordinary transfer.
func TestSpamPricing_InvariantCheckFeeIsPricedAgainstItsWork(t *testing.T) {
	a := newTestApp(t)

	var gs crisistypes.GenesisState
	require.NoError(t, a.AppCodec().UnmarshalJSON(a.DefaultGenesis()[crisistypes.ModuleName], &gs))

	require.Equal(t, cointypes.Denom, gs.ConstantFee.Denom)

	transferFee, ok := math.NewIntFromString(cointypes.DefaultTransferFee)
	require.True(t, ok)
	require.True(t, gs.ConstantFee.Amount.GTE(transferFee.MulRaw(100)),
		"an invariant check scans a whole module's state and must cost far more than a transfer (fee %s, transfer %s)",
		gs.ConstantFee.Amount, transferFee)

	require.Equal(t, math.NewInt(app.InvariantCheckFeeUphi), gs.ConstantFee.Amount)
	t.Logf("invariant-check fee: %s uphi against a %s uphi transfer", gs.ConstantFee.Amount, transferFee)
}

// The shipped genesis must carry these prices.
func TestSpamPricing_ShippedGenesisCarriesTheFee(t *testing.T) {
	a := newTestApp(t)
	raw, ok := a.DefaultGenesis()[crisistypes.ModuleName]
	require.True(t, ok, "the crisis module must be present in the default genesis")

	var decoded map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Contains(t, decoded, "constant_fee")
}

func pinSensitiveThreshold(t *testing.T, a *app.App, ctx sdk.Context, id string) {
	t.Helper()
	inst, found := a.InstitutionsKeeper.GetInstitution(ctx, id)
	require.True(t, found)
	inst.Params.SensitiveThreshold = 1
	a.InstitutionsKeeper.SetInstitution(ctx, inst)
}
