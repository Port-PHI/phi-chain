// SPDX-License-Identifier: Apache-2.0

package ante

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
)

// IdentityStatusSource reports whether a signer controls a non-ACTIVE DID (object-capability over x/identity).
type IdentityStatusSource interface {
	HasNonActiveDID(ctx sdk.Context, controller string) bool
}

// IdentityStatusGuard rejects a tx whose signer controls a SUSPENDED/REVOKED DID; accounts with no DID pass through.
type IdentityStatusGuard struct {
	identity IdentityStatusSource
}

func NewIdentityStatusGuard(src IdentityStatusSource) IdentityStatusGuard {
	return IdentityStatusGuard{identity: src}
}

// AnteHandle rejects the tx when a signer controls a non-ACTIVE DID; skipped on simulate/ReCheckTx (DeliverTx always re-runs it).
func (g IdentityStatusGuard) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if simulate || ctx.IsReCheckTx() {
		return next(ctx, tx, simulate)
	}
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "invalid transaction type for identity status guard")
	}
	signers, err := sigTx.GetSigners()
	if err != nil {
		return ctx, err
	}
	for _, s := range signers {
		addr := sdk.AccAddress(s).String()
		if g.identity.HasNonActiveDID(ctx, addr) {
			return ctx, errorsmod.Wrapf(sdkerrors.ErrUnauthorized,
				"signer %s controls a suspended or revoked phi identity and may not transact", addr)
		}
	}
	return next(ctx, tx, simulate)
}
