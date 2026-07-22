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

// CoinFeeKeeper computes the fixed fee and its governed validator/company split; whole messages are threaded through since fee may depend on message contents.
type CoinFeeKeeper interface {
	ComputeRequiredFee(ctx sdk.Context, msgs []sdk.Msg) math.Int
	ComputeFeeSplit(ctx sdk.Context, msgs []sdk.Msg) cointypes.FeeSplit
	IsMicroExempt(ctx sdk.Context, payer string, msgs []sdk.Msg) bool
	ConsumeMicroExemption(ctx sdk.Context, payer string)
}

// FeeBankKeeper is the bank interface required to deduct the fee.
type FeeBankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, sender sdk.AccAddress, recipientModule string, amt sdk.Coins) error
}

// MaxTxGas is the per-tx gas ceiling: fee is fixed per-message and gas is metered but not priced, so an early ceiling prevents buying unbounded compute for a flat fee.
const MaxTxGas = uint64(2_000_000)

// MaxGasDecorator rejects a tx whose declared gas limit exceeds MaxTxGas; runs at the front of the ante chain, skipped only for simulation.
type MaxGasDecorator struct{}

func NewMaxGasDecorator() MaxGasDecorator { return MaxGasDecorator{} }

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

// FixedFeeDecorator applies a governed fixed per-message fee (not gas-price), with per-DID daily micro-exemption and feegrant.
type FixedFeeDecorator struct {
	ak             authante.AccountKeeper
	bk             FeeBankKeeper
	feegrantKeeper authante.FeegrantKeeper
	coinKeeper     CoinFeeKeeper
}

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

	// Compute the fixed fee and its routing (unless eligible for the daily micro-exemption).
	payer := sdk.AccAddress(feePayer).String()
	split := cointypes.NewFeeSplit()
	exempt := d.coinKeeper.IsMicroExempt(ctx, payer, tx.GetMsgs())
	if !exempt {
		split = d.coinKeeper.ComputeFeeSplit(ctx, tx.GetMsgs())
	}
	required := split.Total
	feeCoins := sdk.NewCoins(sdk.NewCoin(cointypes.Denom, required))

	// feegrant path: deduct from the granter if set; skipped for a zero (micro-exempt) fee.
	if feeGranter != nil && required.IsPositive() {
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

	// Route the fee: both legs are bank SENDS from the payer (Validator+Company == Total), so supply-neutral; an underfunded tx fails one send and the cached store is discarded.
	if required.IsPositive() {
		if split.Validator.IsPositive() {
			if err := d.bk.SendCoinsFromAccountToModule(ctx, deductFrom, authtypes.FeeCollectorName, cointypes.CoinsOf(split.Validator)); err != nil {
				return ctx, errorsmod.Wrapf(sdkerrors.ErrInsufficientFunds, "%s", err)
			}
		}
		if split.Company.IsPositive() {
			if err := d.bk.SendCoinsFromAccountToModule(ctx, deductFrom, cointypes.RevenueAccountName, cointypes.CoinsOf(split.Company)); err != nil {
				return ctx, errorsmod.Wrapf(sdkerrors.ErrInsufficientFunds, "%s", err)
			}
		}
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		sdk.EventTypeTx,
		sdk.NewAttribute(sdk.AttributeKeyFee, feeCoins.String()),
		sdk.NewAttribute(sdk.AttributeKeyFeePayer, sdk.AccAddress(deductFrom).String()),
	))

	// One event per (message type, stream) leg: the only on-chain record separating company from validator cut.
	for _, leg := range split.Legs {
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			cointypes.EventTypeRevenueCollected,
			sdk.NewAttribute(cointypes.AttributeKeyStream, leg.Stream),
			sdk.NewAttribute(cointypes.AttributeKeyMsgType, leg.MsgTypeURL),
			sdk.NewAttribute(cointypes.AttributeKeyAmount, leg.Amount.String()),
		))
	}

	// Consume the per-DID daily micro-exemption quota only on finalize (guard blocks simulate/CheckTx replays), so it commits exactly once on DeliverTx.
	if exempt && !simulate && !ctx.IsCheckTx() {
		d.coinKeeper.ConsumeMicroExemption(ctx, payer)
	}
	return next(ctx, tx, simulate)
}
