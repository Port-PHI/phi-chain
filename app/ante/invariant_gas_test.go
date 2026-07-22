// SPDX-License-Identifier: Apache-2.0

package ante_test

import (
	"testing"

	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	crisistypes "github.com/cosmos/cosmos-sdk/x/crisis/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/app"
	phiante "github.com/Port-PHI/phi-chain/app/ante"
	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
)

func invariantTx(gas uint64) gasTx {
	return gasTx{
		gas:  gas,
		msgs: []sdk.Msg{&crisistypes.MsgVerifyInvariant{Sender: "phi1sender", InvariantModuleName: "identity"}},
	}
}

type txKind struct {
	name string
	tx   gasTx
}

func txKinds() []txKind {
	transfer := &cointypes.MsgTransfer{From: "phi1a", To: "phi1b", Amount: "1000"}
	verify := &crisistypes.MsgVerifyInvariant{Sender: "phi1sender", InvariantModuleName: "identity"}
	return []txKind{
		{name: "bare invariant check", tx: invariantTx(1 << 40)},
		{name: "invariant check bundled with a transfer", tx: gasTx{gas: 1 << 40, msgs: []sdk.Msg{verify, transfer}}},
		{name: "transfer bundled with an invariant check", tx: gasTx{gas: 1 << 40, msgs: []sdk.Msg{transfer, verify}}},
		{name: "two invariant checks", tx: gasTx{gas: 1 << 40, msgs: []sdk.Msg{verify, verify}}},
		{name: "an ordinary transfer", tx: gasTx{gas: 1 << 40, msgs: []sdk.Msg{transfer}}},
		{name: "a tx with no messages at all", tx: gasTx{gas: 1 << 40}},
	}
}

func TestInvariantGas_NoTransactionShapeEscapesTheCeiling(t *testing.T) {
	d := phiante.NewMaxGasDecorator()

	for _, tc := range txKinds() {
		t.Run(tc.name, func(t *testing.T) {
			next := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }
			_, err := d.AnteHandle(
				sdk.Context{}.WithGasMeter(storetypes.NewGasMeter(phiante.MaxTxGas)), tc.tx, false, next)
			require.ErrorIs(t, err, sdkerrors.ErrInvalidGasLimit,
				"%s must be held to the per-tx ceiling", tc.name)
		})
	}
}

// Block-accounting: the decorator must return the given finite meter (an infinite meter reports limit zero).
func TestInvariantGas_TheMeterIsNotReplaced(t *testing.T) {
	d := phiante.NewMaxGasDecorator()

	var inner sdk.Context
	next := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		inner = ctx
		return ctx, nil
	}
	base := sdk.Context{}.WithGasMeter(storetypes.NewGasMeter(phiante.MaxTxGas))

	_, err := d.AnteHandle(base, invariantTx(phiante.MaxTxGas), false, next)
	require.NoError(t, err, "a check within the ceiling is a legitimate transaction")
	require.Equal(t, phiante.MaxTxGas, inner.GasMeter().Limit(),
		"an infinite meter reports a limit of zero, and zero is what the block meter would count")

	require.Panics(t, func() { inner.GasMeter().ConsumeGas(phiante.MaxTxGas+1, "registry scan") },
		"the scan must be bounded by the meter, which is what makes the fee price a bounded operation")
}

// An ordinary transaction is unaffected: the ceiling binds it exactly as before, and a declared limit within the ceiling still passes.
func TestInvariantGas_OrdinaryTransactionsAreUnaffected(t *testing.T) {
	d := phiante.NewMaxGasDecorator()
	next := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }
	transfer := &cointypes.MsgTransfer{From: "phi1a", To: "phi1b", Amount: "1000"}
	base := sdk.Context{}.WithGasMeter(storetypes.NewGasMeter(phiante.MaxTxGas))

	_, err := d.AnteHandle(base, gasTx{gas: phiante.MaxTxGas, msgs: []sdk.Msg{transfer}}, false, next)
	require.NoError(t, err, "a transfer within the ceiling must still pass")

	_, err = d.AnteHandle(base, gasTx{gas: phiante.MaxTxGas + 1, msgs: []sdk.Msg{transfer}}, false, next)
	require.ErrorIs(t, err, sdkerrors.ErrInvalidGasLimit)
}

// The check stays PRICED.
func TestInvariantGas_TheConstantFeeIsStillCharged(t *testing.T) {
	require.Equal(t, int64(1_000_000), app.InvariantCheckFeeUphi)

	transferFee := cointypes.DefaultParams().FeeFor(sdk.MsgTypeURL(&cointypes.MsgTransfer{}))
	require.True(t, transferFee.IsPositive())
	require.Equal(t, int64(200), app.InvariantCheckFeeUphi/transferFee.Int64(),
		"200x an ordinary transfer, now pricing a gas-bounded operation rather than an unbounded one")
}
