// SPDX-License-Identifier: Apache-2.0

package ante

import (
	"fmt"
	"time"

	errorsmod "cosmossdk.io/errors"
	txsigning "cosmossdk.io/x/tx/signing"
	"google.golang.org/protobuf/types/known/anypb"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256r1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"

	"github.com/Port-PHI/phi-chain/phicrypto"
)

// PhiSigVerificationDecorator is the consensus-critical signature-verification step: a fork of the SDK v0.53.4 SigVerificationDecorator that routes a WebAuthn-envelope r1 signer to phi-crypto, with standard k1/r1 taking the verbatim upstream path (exactly one verify per signer).
type PhiSigVerificationDecorator struct {
	ak              ante.AccountKeeper
	signModeHandler *txsigning.HandlerMap
	webauthn        WebAuthnDecorator

	// Upstream options fixed to SDK defaults, matching ante.NewSigVerificationDecorator(ak, handler).
	maxTxTimeoutDuration time.Duration
	unorderedTxGasCost   uint64
}

// NewPhiSigVerificationDecorator builds the WebAuthn-aware sig-verification decorator; a nil verifier falls back to phicrypto.Default() (Disabled without the cgo tag).
func NewPhiSigVerificationDecorator(ak ante.AccountKeeper, signModeHandler *txsigning.HandlerMap, verifier phicrypto.Verifier, params WebAuthnParamSource, uvPolicy UVPolicySource) PhiSigVerificationDecorator {
	return PhiSigVerificationDecorator{
		ak:                   ak,
		signModeHandler:      signModeHandler,
		webauthn:             NewWebAuthnDecorator(verifier, params, uvPolicy),
		maxTxTimeoutDuration: ante.DefaultMaxTimeoutDuration,
		unorderedTxGasCost:   ante.DefaultUnorderedTxGasCost,
	}
}

// AnteHandle mirrors SDK v0.53.4 SigVerificationDecorator.AnteHandle; the only divergence is the marked per-signer branch below.
func (svd PhiSigVerificationDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	sigTx, ok := tx.(authsigning.Tx)
	if !ok {
		return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "invalid transaction type")
	}

	utx, ok := tx.(sdk.TxWithUnordered)
	isUnordered := ok && utx.GetUnordered()
	unorderedEnabled := svd.ak.UnorderedTransactionsEnabled()

	if isUnordered && !unorderedEnabled {
		return ctx, errorsmod.Wrap(sdkerrors.ErrNotSupported, "unordered transactions are not enabled")
	}

	// stdSigs holds sequence/account numbers and signatures (0-length when simulating).
	sigs, err := sigTx.GetSignaturesV2()
	if err != nil {
		return ctx, err
	}

	// Stepped User-Verification: decide once per tx whether it is UV-sensitive under the governed policy (consulted only on the WebAuthn branch below).
	requireUV := svd.webauthn.RequiresUserVerification(ctx, tx.GetMsgs())

	signers, err := sigTx.GetSigners()
	if err != nil {
		return ctx, err
	}

	// check that signer length and signature length are the same
	if len(sigs) != len(signers) {
		return ctx, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "invalid number of signer;  expected: %d, got %d", len(signers), len(sigs))
	}

	// In normal transactions, each signer has a sequence value.
	if isUnordered {
		if err := svd.verifyUnorderedNonce(ctx, utx); err != nil {
			return ctx, err
		}
	}

	for i, sig := range sigs {
		if sig.Sequence > 0 && isUnordered {
			return ctx, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "sequence is not allowed for unordered transactions")
		}
		acc, err := ante.GetSignerAcc(ctx, svd.ak, signers[i])
		if err != nil {
			return ctx, err
		}

		// retrieve pubkey
		pubKey := acc.GetPubKey()
		if !simulate && pubKey == nil {
			return ctx, errorsmod.Wrap(sdkerrors.ErrInvalidPubKey, "pubkey on account is not set")
		}

		// Check account sequence number.
		if !isUnordered {
			if sig.Sequence != acc.GetSequence() {
				return ctx, errorsmod.Wrapf(
					sdkerrors.ErrWrongSequence,
					"account sequence mismatch, expected %d, got %d", acc.GetSequence(), sig.Sequence,
				)
			}
		}

		// retrieve signer data
		genesis := ctx.BlockHeight() == 0
		chainID := ctx.ChainID()
		var accNum uint64
		if !genesis {
			accNum = acc.GetAccountNumber()
		}

		// no need to verify signatures on recheck tx
		if !simulate && !ctx.IsReCheckTx() && ctx.IsSigverifyTx() {
			// An r1 signer whose bytes carry a WebAuthn envelope is a passkey, verified by phi-crypto (P-256-only), requiring BOTH the r1 key AND the envelope magic; anything else falls through to upstream unchanged.
			_, isR1 := pubKey.(*secp256r1.PubKey)
			single, isSingle := sig.Data.(*signing.SingleSignatureData)
			isWebAuthnEnvelope := isSingle && isR1 && IsWebAuthnEnvelope(single.Signature)

			// Stepped-UV enforcement for RAW r1 signers: any account whose key tree contains an r1 leaf (incl.
			if requireUV && !isWebAuthnEnvelope && pubKeyContainsR1(pubKey) {
				return ctx, errorsmod.Wrap(sdkerrors.ErrUnauthorized,
					"user verification required: an account with a secp256r1 key must sign this UV-sensitive transaction via the WebAuthn/UV path, not a raw signature")
			}

			if isWebAuthnEnvelope {
				// Passkeys sign only SIGN_MODE_DIRECT; reject any other mode (malleable amino-JSON).
				if single.SignMode != signing.SignMode_SIGN_MODE_DIRECT {
					return ctx, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "webauthn: only SIGN_MODE_DIRECT is accepted")
				}
				// Derive the same sign-bytes upstream feeds VerifySignature, so the passkey challenge binds this sign-doc (an assertion for tx A cannot authenticate tx B).
				webauthnSignerData := authsigning.SignerData{
					Address:       acc.GetAddress().String(),
					ChainID:       chainID,
					AccountNumber: accNum,
					Sequence:      sig.Sequence,
					PubKey:        pubKey,
				}
				signBytes, err := authsigning.GetSignBytesAdapter(ctx, svd.signModeHandler, single.SignMode, webauthnSignerData, tx)
				if err != nil {
					return ctx, err
				}
				// pubKey.Bytes() is compressed SEC1 P-256 (33 bytes); all crypto checks (challenge, origin allow-list, rpIdHash, UP/UV, low-S) live in phi-crypto.
				handled, verr := svd.webauthn.VerifyEnvelope(ctx, pubKey.Bytes(), signBytes, single.Signature, requireUV)
				if handled {
					if verr != nil {
						return ctx, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "webauthn assertion verification failed")
					}
					continue // accepted; no extra gas (r1 cost already charged)
				}
				// handled == false is unreachable here; fall through to the standard path — never accept.
			}
			anyPk, _ := codectypes.NewAnyWithValue(pubKey)

			signerData := txsigning.SignerData{
				Address:       acc.GetAddress().String(),
				ChainID:       chainID,
				AccountNumber: accNum,
				Sequence:      sig.Sequence,
				PubKey: &anypb.Any{
					TypeUrl: anyPk.TypeUrl,
					Value:   anyPk.Value,
				},
			}
			adaptableTx, ok := tx.(authsigning.V2AdaptableTx)
			if !ok {
				return ctx, fmt.Errorf("expected tx to implement V2AdaptableTx, got %T", tx)
			}
			txData := adaptableTx.GetSigningTxData()
			err = authsigning.VerifySignature(ctx, pubKey, signerData, sig.Data, svd.signModeHandler, txData)
			if err != nil {
				var errMsg string
				if ante.OnlyLegacyAminoSigners(sig.Data) {
					// If all signers are using SIGN_MODE_LEGACY_AMINO, we rely on VerifySignature to check account sequence number, and therefore communicate sequence number as a potential cause of error.
					errMsg = fmt.Sprintf("signature verification failed; please verify account number (%d), sequence (%d) and chain-id (%s)", accNum, acc.GetSequence(), chainID)
				} else {
					errMsg = fmt.Sprintf("signature verification failed; please verify account number (%d) and chain-id (%s): (%s)", accNum, chainID, err.Error())
				}
				return ctx, errorsmod.Wrap(sdkerrors.ErrUnauthorized, errMsg)

			}
		}
	}

	return next(ctx, tx, simulate)
}

func (svd PhiSigVerificationDecorator) verifyUnorderedNonce(ctx sdk.Context, unorderedTx sdk.TxWithUnordered) error {
	blockTime := ctx.BlockTime()
	timeoutTimestamp := unorderedTx.GetTimeoutTimeStamp()
	if timeoutTimestamp.IsZero() || timeoutTimestamp.Unix() == 0 {
		return errorsmod.Wrap(
			sdkerrors.ErrInvalidRequest,
			"unordered transaction must have timeout_timestamp set",
		)
	}
	if timeoutTimestamp.Before(blockTime) {
		return errorsmod.Wrap(
			sdkerrors.ErrInvalidRequest,
			"unordered transaction has a timeout_timestamp that has already passed",
		)
	}
	if timeoutTimestamp.After(blockTime.Add(svd.maxTxTimeoutDuration)) {
		return errorsmod.Wrapf(
			sdkerrors.ErrInvalidRequest,
			"unordered tx ttl exceeds %s",
			svd.maxTxTimeoutDuration.String(),
		)
	}

	ctx.GasMeter().ConsumeGas(svd.unorderedTxGasCost, "unordered tx")

	execMode := ctx.ExecMode()
	if execMode == sdk.ExecModeSimulate {
		return nil
	}

	sigTx, ok := unorderedTx.(authsigning.SigVerifiableTx)
	if !ok {
		return errorsmod.Wrap(sdkerrors.ErrTxDecode, "invalid tx type")
	}
	signerAddrs, err := sigTx.GetSigners()
	if err != nil {
		return err
	}

	for _, signerAddr := range signerAddrs {
		if err := svd.ak.TryAddUnorderedNonce(ctx, signerAddr, unorderedTx.GetTimeoutTimeStamp()); err != nil {
			return errorsmod.Wrapf(
				sdkerrors.ErrInvalidRequest,
				"failed to add unordered nonce: %s", err,
			)
		}
	}

	return nil
}
