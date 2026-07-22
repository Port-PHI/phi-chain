// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"context"

	"cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/Port-PHI/phi-chain/x/coin/types"
)

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns a MsgServer implementation.
func NewMsgServerImpl(k Keeper) types.MsgServer {
	return &msgServer{Keeper: k}
}

var _ types.MsgServer = msgServer{}

// Transfer performs a peer-to-peer uphi transfer.
func (k msgServer) Transfer(goCtx context.Context, msg *types.MsgTransfer) (*types.MsgTransferResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	from, err := sdk.AccAddressFromBech32(msg.From)
	if err != nil {
		return nil, err
	}
	to, err := sdk.AccAddressFromBech32(msg.To)
	if err != nil {
		return nil, err
	}
	amount, ok := math.NewIntFromString(msg.Amount)
	if !ok || !amount.IsPositive() {
		return nil, errors.Wrapf(types.ErrInvalidAmount, "amount: %q", msg.Amount)
	}

	// 1) Move the coin.
	if err := k.bankKeeper.SendCoins(ctx, from, to, types.CoinsOf(amount)); err != nil {
		return nil, err
	}

	// 2) Move the AGE with it.
	for _, lot := range k.SpendOldestFirst(ctx, msg.From, amount) {
		k.AddCoins(ctx, msg.To, types.LotAmount(lot), lot.AcquiredAt)
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeTransfer,
		sdk.NewAttribute(types.AttributeKeyFrom, msg.From),
		sdk.NewAttribute(types.AttributeKeyTo, msg.To),
		sdk.NewAttribute(types.AttributeKeyAmount, amount.String()),
	))
	return &types.MsgTransferResponse{Burned: "0"}, nil
}

// WithdrawRevenue moves accrued revenue out of the keyless phi_revenue module account and into the governed company_payout_address; governance authority only.
func (k msgServer) WithdrawRevenue(goCtx context.Context, msg *types.MsgWithdrawRevenue) (*types.MsgWithdrawRevenueResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if msg.Authority != k.authority {
		return nil, errors.Wrapf(govtypes.ErrInvalidSigner, "expected %s, got %s", k.authority, msg.Authority)
	}
	amount, ok := math.NewIntFromString(msg.Amount)
	if !ok || !amount.IsPositive() {
		return nil, errors.Wrapf(types.ErrInvalidAmount, "amount: %q", msg.Amount)
	}

	dest := k.GetParams(ctx).CompanyPayoutAddress
	if dest == "" {
		return nil, types.ErrNoPayoutAddress
	}
	to, err := sdk.AccAddressFromBech32(dest)
	if err != nil {
		return nil, errors.Wrapf(sdkerrors.ErrInvalidAddress, "company_payout_address %q: %s", dest, err)
	}

	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.RevenueAccountName, to, types.CoinsOf(amount)); err != nil {
		return nil, errors.Wrapf(types.ErrInsufficientFunds, "%s", err)
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRevenueWithdrawn,
		sdk.NewAttribute(types.AttributeKeyTo, dest),
		sdk.NewAttribute(types.AttributeKeyAmount, amount.String()),
	))
	return &types.MsgWithdrawRevenueResponse{}, nil
}

// UpdateParams sets parameters; governance authority only.
func (k msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if msg.Authority != k.authority {
		return nil, errors.Wrapf(govtypes.ErrInvalidSigner, "expected %s, got %s", k.authority, msg.Authority)
	}
	if err := k.SetParams(ctx, msg.Params); err != nil {
		return nil, err
	}
	return &types.MsgUpdateParamsResponse{}, nil
}
