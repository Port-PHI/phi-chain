// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/phicrypto"
	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	"github.com/Port-PHI/phi-chain/x/identity/types"
)

func (k Keeper) verifyReauthAttestation(ctx sdk.Context, msg *types.MsgInitiateRecovery, doc types.DIDDocument) error {
	attestor, found := k.GetTrustedIssuer(ctx, msg.AttestorDid)
	if !found || !attestor.Active {
		return errors.Wrapf(types.ErrIssuerNotTrusted, "attestor %s", msg.AttestorDid)
	}
	// doc.UniquenessHash from state (never msg); ctx.ChainID() binds to this network (anti-replay).
	m := types.ReauthAttestationMessage(ctx.ChainID(), msg.Did, msg.ProposedNewPubKey, msg.Creator, doc.UniquenessHash, msg.Nonce)
	if !k.verifier.VerifySignature(phicrypto.Secp256r1, attestor.PubKey, m, msg.ReauthAttestation) {
		return errors.Wrap(types.ErrInvalidReauthAttestation, "re-auth attestation did not verify")
	}
	return nil
}

func (k Keeper) chargeReauthFee(ctx sdk.Context, from sdk.AccAddress, amount math.Int) error {
	if !amount.IsPositive() {
		return nil
	}
	return k.bankKeeper.SendCoinsFromAccountToModule(ctx, from, types.FeeCollectorName, cointypes.CoinsOf(amount))
}
