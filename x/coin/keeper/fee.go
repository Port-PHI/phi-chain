// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/coin/types"
)

// ComputeRequiredFee returns the total fixed fee (in uphi) for a transaction's messages.
func (k Keeper) ComputeRequiredFee(ctx sdk.Context, msgs []sdk.Msg) math.Int {
	return k.ComputeFeeSplit(ctx, msgs).Total
}

// FeeForMsg returns the fee owed by a single message: governed per-type amount plus any content surcharge.
func (k Keeper) FeeForMsg(ctx sdk.Context, msg sdk.Msg) math.Int {
	return k.GetParams(ctx).FeeForMsg(msg)
}

// ComputeFeeSplit computes the fixed fee and routes each message's fee through the governed split table (pure read).
func (k Keeper) ComputeFeeSplit(ctx sdk.Context, msgs []sdk.Msg) types.FeeSplit {
	params := k.GetParams(ctx)
	out := types.NewFeeSplit()
	for _, m := range msgs {
		url := sdk.MsgTypeURL(m)
		fee := params.FeeForMsg(m)
		if !fee.IsPositive() {
			continue
		}
		entry, _ := params.SplitFor(url)
		company, validator := entry.CompanyShare(fee)

		out.Total = out.Total.Add(fee)
		out.Company = out.Company.Add(company)
		out.Validator = out.Validator.Add(validator)
		if validator.IsPositive() {
			out.Legs = append(out.Legs, types.RevenueLeg{MsgTypeURL: url, Stream: types.StreamValidator, Amount: validator})
		}
		if company.IsPositive() {
			out.Legs = append(out.Legs, types.RevenueLeg{MsgTypeURL: url, Stream: types.StreamCompany, Amount: company})
		}
	}
	return out
}

func (k Keeper) microExemptionSubject(ctx sdk.Context, address string) (string, bool) {
	if k.identityKeeper == nil {
		return "", false
	}
	did, ok := k.identityKeeper.SubjectDID(ctx, address)
	if !ok || did == "" {
		return "", false
	}
	return did, true
}

// IsMicroExempt reports whether the tx qualifies for the daily micro-fee exemption: EXACTLY ONE MsgTransfer below threshold, both payer and recipient identified humans, payer DID still has quota today.
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
	subject, ok := k.microExemptionSubject(ctx, payer)
	if !ok {
		return false
	}
	if _, ok := k.microExemptionSubject(ctx, t.To); !ok {
		return false
	}
	day := ctx.BlockTime().Unix() / 86400
	return k.GetMicroUsed(ctx, day, subject) < params.MicroDailyQuota
}

// ConsumeMicroExemption records one use of the payer's daily quota, keyed by DID (not address).
func (k Keeper) ConsumeMicroExemption(ctx sdk.Context, payer string) {
	subject, ok := k.microExemptionSubject(ctx, payer)
	if !ok {
		return
	}
	day := ctx.BlockTime().Unix() / 86400
	k.IncrMicroUsed(ctx, day, subject)
}
