// SPDX-License-Identifier: Apache-2.0

package ante_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	coreaddress "cosmossdk.io/core/address"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	phiante "github.com/Port-PHI/phi-chain/app/ante"
	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
)

type gasTx struct {
	sdk.Tx
	gas  uint64
	msgs []sdk.Msg
}

func (t gasTx) GetGas() uint64     { return t.gas }
func (t gasTx) GetFee() sdk.Coins  { return nil }
func (t gasTx) FeePayer() []byte   { return nil }
func (t gasTx) FeeGranter() []byte { return nil }
func (t gasTx) GetMsgs() []sdk.Msg { return t.msgs }

// A tx declaring more gas than the per-tx ceiling is rejected up-front by MaxGasDecorator.
func TestMaxGas_RejectsTxOverGasCeiling(t *testing.T) {
	d := phiante.NewMaxGasDecorator()
	noop := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }

	_, err := d.AnteHandle(sdk.Context{}, gasTx{gas: phiante.MaxTxGas + 1}, false, noop)
	require.ErrorIs(t, err, sdkerrors.ErrInvalidGasLimit, "gas limit just over the ceiling must be rejected")

	_, err = d.AnteHandle(sdk.Context{}, gasTx{gas: 1 << 60}, false, noop)
	require.ErrorIs(t, err, sdkerrors.ErrInvalidGasLimit, "an enormous gas limit must be rejected")

	_, err = d.AnteHandle(sdk.Context{}, gasTx{gas: phiante.MaxTxGas}, false, noop)
	require.NoError(t, err)
	_, err = d.AnteHandle(sdk.Context{}, gasTx{gas: 1 << 60}, true, noop)
	require.NoError(t, err)
}

type feeTx struct {
	sdk.Tx
	payer, granter []byte
}

func (t feeTx) GetGas() uint64     { return 0 }
func (t feeTx) GetFee() sdk.Coins  { return nil }
func (t feeTx) FeePayer() []byte   { return t.payer }
func (t feeTx) FeeGranter() []byte { return t.granter }
func (t feeTx) GetMsgs() []sdk.Msg { return nil }

type fakeAccountKeeper struct{}

func (fakeAccountKeeper) GetParams(context.Context) authtypes.Params { return authtypes.Params{} }
func (fakeAccountKeeper) GetAccount(_ context.Context, addr sdk.AccAddress) sdk.AccountI {
	return authtypes.NewBaseAccountWithAddress(addr)
}
func (fakeAccountKeeper) SetAccount(context.Context, sdk.AccountI) {}
func (fakeAccountKeeper) GetModuleAddress(string) sdk.AccAddress {
	return sdk.AccAddress([]byte("fee_collector_______"))
}
func (fakeAccountKeeper) AddressCodec() coreaddress.Codec                { return nil }
func (fakeAccountKeeper) UnorderedTransactionsEnabled() bool             { return false }
func (fakeAccountKeeper) RemoveExpiredUnorderedNonces(sdk.Context) error { return nil }
func (fakeAccountKeeper) TryAddUnorderedNonce(sdk.Context, []byte, time.Time) error {
	return nil
}

type fakeFeegrant struct{ called bool }

func (f *fakeFeegrant) UseGrantedFees(context.Context, sdk.AccAddress, sdk.AccAddress, sdk.Coins, []sdk.Msg) error {
	f.called = true
	return fmt.Errorf("feegrant must not be consulted for a zero-fee tx")
}

type fakeCoinKeeper struct{}

func (fakeCoinKeeper) ComputeRequiredFee(sdk.Context, []sdk.Msg) math.Int { return math.ZeroInt() }
func (fakeCoinKeeper) ComputeFeeSplit(sdk.Context, []sdk.Msg) cointypes.FeeSplit {
	return cointypes.NewFeeSplit()
}
func (fakeCoinKeeper) IsMicroExempt(sdk.Context, string, []sdk.Msg) bool { return true }
func (fakeCoinKeeper) ConsumeMicroExemption(sdk.Context, string)         {}

type fakeFeeBank struct{}

func (fakeFeeBank) SendCoinsFromAccountToModule(context.Context, sdk.AccAddress, string, sdk.Coins) error {
	return nil
}

// A micro-exempt (zero-fee) tx that also names a fee granter must pass WITHOUT calling the feegrant keeper: UseGrantedFees with empty coins can reject a legitimately-free tx.
func TestFixedFee_SkipsFeegrantForZeroFee(t *testing.T) {
	fg := &fakeFeegrant{}
	d := phiante.NewFixedFeeDecorator(fakeAccountKeeper{}, fakeFeeBank{}, fg, fakeCoinKeeper{})

	key := storetypes.NewKVStoreKey("t_fee")
	ctx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_fee_t")).Ctx.
		WithEventManager(sdk.NewEventManager())
	tx := feeTx{
		payer:   sdk.AccAddress([]byte("payer_______________")),
		granter: sdk.AccAddress([]byte("granter_____________")),
	}
	noop := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }

	_, err := d.AnteHandle(ctx, tx, false, noop)
	require.NoError(t, err, "a micro-exempt tx with a granter set must still pass")
	require.False(t, fg.called, "feegrant must not be consulted when the required fee is zero")
}

// The per-transaction gas ceiling has to price out spam WITHOUT pricing out ordinary use.
func TestMaxGas_OrdinaryTransactionsStillFit(t *testing.T) {
	d := phiante.NewMaxGasDecorator()
	noop := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }

	for _, tc := range []struct {
		name string
		gas  uint64
	}{
		{"identity registration", 200_000},
		{"institution mint", 200_000},
		{"institution redemption", 200_000},
		{"social recovery execution", 250_000},
		{"a transaction carrying twenty messages", 1_000_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := d.AnteHandle(sdk.Context{}, gasTx{gas: tc.gas}, false, noop)
			require.NoError(t, err, "ordinary work must still fit under the per-transaction gas ceiling")
		})
	}

	require.GreaterOrEqual(t, phiante.MaxTxGas, uint64(52_425)*20)
}
