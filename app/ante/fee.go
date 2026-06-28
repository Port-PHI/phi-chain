// SPDX-License-Identifier: Apache-2.0

package ante

import (
	"bytes"
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
)

// CoinFeeKeeper is the interface required from the coin module to compute the fixed fee.
type CoinFeeKeeper interface {
	ComputeRequiredFee(ctx sdk.Context, msgs []sdk.Msg) math.Int
	IsMicroExempt(ctx sdk.Context, payer string, msgs []sdk.Msg) bool
	ConsumeMicroExemption(ctx sdk.Context, payer string)
}

// FeeBankKeeper is the bank interface required to deduct the fee.
type FeeBankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, sender sdk.AccAddress, recipientModule string, amt sdk.Coins) error
}

// MaxTxGas is the per-transaction gas ceiling. Because the fee is a fixed per-message amount
// and gas is metered but not priced, without a ceiling a single tx could declare an enormous gas
// limit and buy unbounded validator compute for the flat fee. A tx whose declared gas limit exceeds
// this ceiling is rejected. The value is generous (≫ an ordinary tx) and far below DefaultBlockMaxGas,
// so a block still holds many ordinary txs while no single tx can monopolize it.
const MaxTxGas = uint64(10_000_000)

// MaxGasDecorator rejects a tx whose declared gas limit exceeds MaxTxGas. It runs at the FRONT of the
// ante chain (right after SetUpContext) so the ceiling is enforced BEFORE any gas-consuming decorator
// With a fixed per-message fee, gas is metered but not priced, so without an early
// ceiling a tx could declare an enormous gas limit and buy unbounded validator compute for the flat
// fee. Skipped only for gas-estimation simulation, which intentionally probes high gas limits.
type MaxGasDecorator struct{}

// NewMaxGasDecorator is the constructor.
func NewMaxGasDecorator() MaxGasDecorator { return MaxGasDecorator{} }

// AnteHandle enforces the per-tx gas ceiling.
func (MaxGasDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
	}
	if !simulate && feeTx.GetGas() > MaxTxGas {
		return ctx, errorsmod.Wrapf(sdkerrors.ErrInvalidGasLimit, "gas limit %d exceeds the per-tx ceiling %d", feeTx.GetGas(), MaxTxGas)
	}
	return next(ctx, tx, simulate)
}

// FixedFeeDecorator applies a fixed per-message fee instead of a gas-price.
// The fee table is read from the coin module Params (0.005 PHI per transfer; micro-exemption
// below 0.05 PHI with a per-DID daily quota). Rates change only via governance — this logic is
// consensus-critical. It supports feegrant (sponsoring a new user, bound to a unique DID).
type FixedFeeDecorator struct {
	ak             authante.AccountKeeper
	bk             FeeBankKeeper
	feegrantKeeper authante.FeegrantKeeper
	coinKeeper     CoinFeeKeeper
}

// NewFixedFeeDecorator is the constructor.
func NewFixedFeeDecorator(ak authante.AccountKeeper, bk FeeBankKeeper, fk authante.FeegrantKeeper, ck CoinFeeKeeper) FixedFeeDecorator {
	return FixedFeeDecorator{ak: ak, bk: bk, feegrantKeeper: fk, coinKeeper: ck}
}

func (d FixedFeeDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
	}

	if addr := d.ak.GetModuleAddress(authtypes.FeeCollectorName); addr == nil {
		return ctx, fmt.Errorf("fee collector module account (%s) has not been set", authtypes.FeeCollectorName)
	}

	feePayer := feeTx.FeePayer()
	feeGranter := feeTx.FeeGranter()
	deductFrom := feePayer

	// Compute the fixed fee (unless eligible for the daily micro-exemption).
	payer := sdk.AccAddress(feePayer).String()
	required := math.ZeroInt()
	exempt := d.coinKeeper.IsMicroExempt(ctx, payer, tx.GetMsgs())
	if !exempt {
		required = d.coinKeeper.ComputeRequiredFee(ctx, tx.GetMsgs())
	}
	feeCoins := sdk.NewCoins(sdk.NewCoin(cointypes.Denom, required))

	// feegrant path: deduct from the granter if one is set.
	if feeGranter != nil {
		feeGranterAddr := sdk.AccAddress(feeGranter)
		if d.feegrantKeeper == nil {
			return ctx, sdkerrors.ErrInvalidRequest.Wrap("fee grants are not enabled")
		} else if !bytes.Equal(feeGranterAddr, feePayer) {
			if err := d.feegrantKeeper.UseGrantedFees(ctx, feeGranterAddr, feePayer, feeCoins, tx.GetMsgs()); err != nil {
				return ctx, errorsmod.Wrapf(err, "%s does not allow to pay fees for %s", feeGranter, feePayer)
			}
		}
		deductFrom = feeGranterAddr
	}

	deductAcc := d.ak.GetAccount(ctx, deductFrom)
	if deductAcc == nil {
		return ctx, sdkerrors.ErrUnknownAddress.Wrapf("fee payer address: %s does not exist", deductFrom)
	}

	if required.IsPositive() {
		if err := d.bk.SendCoinsFromAccountToModule(ctx, deductFrom, authtypes.FeeCollectorName, feeCoins); err != nil {
			return ctx, errorsmod.Wrapf(sdkerrors.ErrInsufficientFunds, "%s", err)
		}
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		sdk.EventTypeTx,
		sdk.NewAttribute(sdk.AttributeKeyFee, feeCoins.String()),
		sdk.NewAttribute(sdk.AttributeKeyFeePayer, sdk.AccAddress(deductFrom).String()),
	))

	// Consume the per-DID daily micro-exemption quota only on a finalize-mode execution. The guard
	// !simulate && !ctx.IsCheckTx() blocks gas-estimation (simulate) and mempool CheckTx/ReCheckTx -
	// which both replay the ante - from burning the quota. The proposal phases also pass the guard
	// but run on throwaway state, so the quota is committed exactly once, on the DeliverTx path.
	if exempt && !simulate && !ctx.IsCheckTx() {
		d.coinKeeper.ConsumeMicroExemption(ctx, payer)
	}
	return next(ctx, tx, simulate)
}
