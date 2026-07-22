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

// RejectUnsafeGovParamsDecorator refuses at ingress a tx arming a gov deposit-burn flag (or clearing ProposalCancelDest) while any vault is non-zero: burning vault-backed uphi would break solvency and halt the chain (defense-in-depth; the burn-site guard is the other half).
type RejectUnsafeGovParamsDecorator struct {
	vaults VaultReader
}

// NewRejectUnsafeGovParamsDecorator builds the decorator over the institution vault reader.
func NewRejectUnsafeGovParamsDecorator(vaults VaultReader) RejectUnsafeGovParamsDecorator {
	return RejectUnsafeGovParamsDecorator{vaults: vaults}
}

// AnteHandle scans the transaction's messages (unwrapping a governance proposal) and refuses one that would arm a uphi deposit burn while vaults are non-zero.
func (d RejectUnsafeGovParamsDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	for _, msg := range tx.GetMsgs() {
		if err := d.check(ctx, msg); err != nil {
			return ctx, err
		}
	}
	return next(ctx, tx, simulate)
}

func (d RejectUnsafeGovParamsDecorator) check(ctx sdk.Context, msg sdk.Msg) error {
	switch m := msg.(type) {
	case *govv1.MsgSubmitProposal:
		inner, err := m.GetMsgs()
		if err != nil {
			return nil // unpack failure is rejected by ValidateBasic
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

func enablesDepositBurn(p govv1.Params) bool {
	return p.BurnVoteVeto || p.BurnVoteQuorum || p.BurnProposalDepositPrevote || p.ProposalCancelDest == ""
}
