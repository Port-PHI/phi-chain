// SPDX-License-Identifier: Apache-2.0

package ante_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"

	phiante "github.com/Port-PHI/phi-chain/app/ante"
)

// gasTx is a minimal FeeTx that only carries a gas limit; the per-tx gas-ceiling check runs before any
// keeper is touched, so the embedded (nil) Tx methods are never called on the rejection path.
type gasTx struct {
	sdk.Tx
	gas uint64
}

func (t gasTx) GetGas() uint64     { return t.gas }
func (t gasTx) GetFee() sdk.Coins  { return nil }
func (t gasTx) FeePayer() []byte   { return nil }
func (t gasTx) FeeGranter() []byte { return nil }

// A tx that declares more gas than the per-tx ceiling is rejected by the dedicated MaxGasDecorator,
// which runs at the front of the ante chain, so the fixed per-message fee cannot buy
// unbounded validator compute and the ceiling is enforced before any gas-consuming decorator.
func TestMaxGas_RejectsTxOverGasCeiling(t *testing.T) {
	d := phiante.NewMaxGasDecorator()
	noop := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }

	_, err := d.AnteHandle(sdk.Context{}, gasTx{gas: phiante.MaxTxGas + 1}, false, noop)
	require.ErrorIs(t, err, sdkerrors.ErrInvalidGasLimit, "gas limit just over the ceiling must be rejected")

	_, err = d.AnteHandle(sdk.Context{}, gasTx{gas: 1 << 60}, false, noop)
	require.ErrorIs(t, err, sdkerrors.ErrInvalidGasLimit, "an enormous gas limit must be rejected")

	// At the ceiling passes through; simulate (gas estimation) is exempt.
	_, err = d.AnteHandle(sdk.Context{}, gasTx{gas: phiante.MaxTxGas}, false, noop)
	require.NoError(t, err)
	_, err = d.AnteHandle(sdk.Context{}, gasTx{gas: 1 << 60}, true, noop)
	require.NoError(t, err)
}
