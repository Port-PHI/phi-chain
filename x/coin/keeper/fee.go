// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/coin/types"
)

// ComputeRequiredFee returns the total fixed fee (in uphi) for a transaction's messages.
func (k Keeper) ComputeRequiredFee(ctx sdk.Context, msgs []sdk.Msg) math.Int {
	params := k.GetParams(ctx)
	total := math.ZeroInt()
	for _, m := range msgs {
		total = total.Add(params.FeeFor(sdk.MsgTypeURL(m)))
	}
	return total
}

// IsMicroExempt reports whether the transaction qualifies for the daily micro-fee exemption.
// Condition: the tx carries EXACTLY ONE message, a MsgTransfer below the threshold amount, and the
// payer's daily quota remains. The single-message rule closes the gap where bundling several
// sub-threshold transfers into one tx must not ride a single quota decrement and all go free; each
// micro transfer is its own tx and spends one quota slot, so the per-day free-transfer count is the
// quota, not quota×(messages per tx).
// This is a pure read with NO side effect: the quota is consumed separately via ConsumeMicroExemption,
// which the ante calls only on the final delivery path so simulate/CheckTx never burn the quota.
// The quota is keyed by the payer ADDRESS. In Phi each human holds one account, so the address is a
// per-DID surrogate; true per-DID keying (resolving address→DID via the identity keeper) is a
// consensus change deferred to the live-router phase.
func (k Keeper) IsMicroExempt(ctx sdk.Context, payer string, msgs []sdk.Msg) bool {
	params := k.GetParams(ctx)
	if params.MicroDailyQuota == 0 {
		return false
	}
	if len(msgs) != 1 {
		return false
	}
	t, ok := msgs[0].(*types.MsgTransfer)
	if !ok {
		return false
	}
	amt, ok2 := math.NewIntFromString(t.Amount)
	if !ok2 || amt.GTE(params.MicroThresholdInt()) {
		return false
	}
	day := ctx.BlockTime().Unix() / 86400
	return k.GetMicroUsed(ctx, day, payer) < params.MicroDailyQuota
}

// ConsumeMicroExemption records one use of the payer's daily micro-exemption quota. It MUST be
// called only on the final delivery path (DeliverTx); the ante guards it with !simulate &&
// !ctx.IsCheckTx() so gas-estimation (simulate) and mempool CheckTx/ReCheckTx - which replay the
// ante repeatedly - never consume the per-DID daily quota.
func (k Keeper) ConsumeMicroExemption(ctx sdk.Context, payer string) {
	day := ctx.BlockTime().Unix() / 86400
	k.IncrMicroUsed(ctx, day, payer)
}

// DeductFees transfers the fixed fee from the payer to the fee collector.
func (k Keeper) DeductFees(ctx sdk.Context, payer sdk.AccAddress, fee math.Int) error {
	if !fee.IsPositive() {
		return nil
	}
	return k.bankKeeper.SendCoinsFromAccountToModule(ctx, payer, types.FeeCollectorName, types.CoinsOf(fee))
}
