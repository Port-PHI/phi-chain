// SPDX-License-Identifier: Apache-2.0

package ante

import (
	"cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
)

// VaultReader reports the total institution vault balance — the Rial backing behind every uphi.
type VaultReader interface {
	SumVaultBalance(ctx sdk.Context) math.Int
}

// RejectUnsafeGovParamsDecorator rejects a transaction that would enable a governance deposit-burn flag
// (BurnVoteVeto / BurnVoteQuorum / BurnProposalDepositPrevote) or clear ProposalCancelDest while any
// institution vault still holds a balance (defense-in-depth).
//
// uphi is the vault-backed deposit/bond denom. Burning governance deposits while the peg is live would
// shrink TotalSupply below Σvault×1e6/rate and break the solvency invariant; and because gov's
// DeleteAndBurnDeposits error propagates out of the EndBlocker — and the institutions EndBlock asserts
// solvency fail-closed — such a burn could brick the chain. So the unsafe transition is refused at
// ingress instead, the only halt-safe place to stop it (an error at burn time would itself halt). These
// flags may still be enabled while every vault is empty (e.g. before launch). The change normally
// arrives wrapped in a MsgSubmitProposal, which this decorator unwraps; it is a no-op unless such a param
// change is present and a vault is non-zero.
type RejectUnsafeGovParamsDecorator struct {
	vaults VaultReader
}

// NewRejectUnsafeGovParamsDecorator builds the decorator over the institution vault reader.
func NewRejectUnsafeGovParamsDecorator(vaults VaultReader) RejectUnsafeGovParamsDecorator {
	return RejectUnsafeGovParamsDecorator{vaults: vaults}
}

// AnteHandle scans the transaction's messages (unwrapping a governance proposal) and refuses one that
// would arm a uphi deposit burn while vaults are non-zero.
func (d RejectUnsafeGovParamsDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	for _, msg := range tx.GetMsgs() {
		if err := d.check(ctx, msg); err != nil {
			return ctx, err
		}
	}
	return next(ctx, tx, simulate)
}

// check rejects an unsafe gov MsgUpdateParams, descending into a MsgSubmitProposal's inner messages.
//
// Legacy path: the gov deposit-burn flags are v1 gov params, changeable ONLY via
// MsgUpdateParams (covered below). The app's legacy v1beta1 router registers just the gov
// text-proposal handler (app.go) — no params ParameterChangeProposal route — so a v1beta1
// proposal / MsgExecLegacyContent cannot reach these params. There is therefore no legacy backdoor to
// guard; were a param-change route ever added, this guard must be extended to unwrap it.
func (d RejectUnsafeGovParamsDecorator) check(ctx sdk.Context, msg sdk.Msg) error {
	switch m := msg.(type) {
	case *govv1.MsgSubmitProposal:
		inner, err := m.GetMsgs()
		if err != nil {
			// A proposal whose messages cannot be unpacked is rejected by ValidateBasic; not our concern.
			return nil
		}
		for _, im := range inner {
			if err := d.check(ctx, im); err != nil {
				return err
			}
		}
	case *govv1.MsgUpdateParams:
		if enablesDepositBurn(m.Params) && d.vaults.SumVaultBalance(ctx).IsPositive() {
			return errors.Wrap(sdkerrors.ErrInvalidRequest,
				"governance deposit-burn (BurnVoteVeto/BurnVoteQuorum/BurnProposalDepositPrevote or an empty "+
					"ProposalCancelDest) may not be enabled while any institution vault is non-zero: uphi is "+
					"vault-backed, so burning deposits would break the solvency invariant")
		}
	}
	return nil
}

// enablesDepositBurn reports whether the proposed gov params would burn uphi deposits: any deposit-burn
// flag set, or the cancellation destination cleared (which routes the cancellation charge to a burn).
func enablesDepositBurn(p govv1.Params) bool {
	return p.BurnVoteVeto || p.BurnVoteQuorum || p.BurnProposalDepositPrevote || p.ProposalCancelDest == ""
}
