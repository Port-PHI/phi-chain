// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"context"

	"cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
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

// Transfer performs a peer-to-peer uphi transfer - no burn/demurrage.
// The recipient receives the full amount; only the **coin-age label** travels with the transfer
// (anti-circumvention), so the real coin age cannot be gamed when redeeming to an institution.
// (The tiered burn applies only in InstitutionRedeem and to the paid toman, not here.)
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

	params := k.GetParams(ctx)
	now := ctx.BlockTime().Unix()

	// 1) Mature the sender's buckets and determine the young/old mix.
	fromCA := MatureCoinAge(k.GetCoinAge(ctx, msg.From), now, params.CoinAgeThresholdSeconds)
	young := mustInt(fromCA.YoungAmount)
	old := mustInt(fromCA.OldAmount)
	tracked := young.Add(old)
	// Untracked coins (e.g. from paths outside this module) are assumed old.
	if tracked.LT(amount) {
		old = old.Add(amount.Sub(tracked))
		tracked = young.Add(old)
	}

	// 2) Spend the buckets proportionally (to preserve the age label across the transfer).
	youngSpent := math.ZeroInt()
	if tracked.IsPositive() {
		youngSpent = amount.Mul(young).Quo(tracked)
	}
	oldSpent := amount.Sub(youngSpent)

	// 3) Transfer the full amount (no deduction).
	if err := k.bankKeeper.SendCoins(ctx, from, to, types.CoinsOf(amount)); err != nil {
		return nil, err
	}

	// 4) Decrement the sender's bucket.
	fromCA.YoungAmount = young.Sub(youngSpent).String()
	fromCA.OldAmount = old.Sub(oldSpent).String()
	k.SetCoinAge(ctx, fromCA)

	// 5) Add to the recipient's bucket, preserving the age label (anti-circumvention - coin age is not reset).
	if youngSpent.IsPositive() {
		k.AddYoungCoins(ctx, msg.To, youngSpent, fromCA.YoungSince)
	}
	if oldSpent.IsPositive() {
		k.addOldCoins(ctx, msg.To, oldSpent)
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeTransfer,
		sdk.NewAttribute(types.AttributeKeyFrom, msg.From),
		sdk.NewAttribute(types.AttributeKeyTo, msg.To),
		sdk.NewAttribute(types.AttributeKeyAmount, amount.String()),
	))
	return &types.MsgTransferResponse{Burned: "0"}, nil
}

// addOldCoins adds an amount to an address's old bucket.
func (k Keeper) addOldCoins(ctx sdk.Context, address string, amount math.Int) {
	ca := MatureCoinAge(k.GetCoinAge(ctx, address), ctx.BlockTime().Unix(), k.GetParams(ctx).CoinAgeThresholdSeconds)
	ca.OldAmount = mustInt(ca.OldAmount).Add(amount).String()
	k.SetCoinAge(ctx, ca)
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
