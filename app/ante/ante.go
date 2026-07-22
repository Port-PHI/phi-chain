// SPDX-License-Identifier: Apache-2.0

package ante

import (
	"errors"

	txsigning "cosmossdk.io/x/tx/signing"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"

	"github.com/Port-PHI/phi-chain/phicrypto"
)

// HandlerOptions holds the options for building the Phi AnteHandler.
type HandlerOptions struct {
	AccountKeeper   ante.AccountKeeper
	BankKeeper      FeeBankKeeper
	FeegrantKeeper  ante.FeegrantKeeper
	SignModeHandler *txsigning.HandlerMap
	CoinKeeper      CoinFeeKeeper
	// Verifier is the phi-crypto port the WebAuthn slot is bound to (Disabled until the cgo link).
	Verifier phicrypto.Verifier
	// WebAuthnParams sources the governed relying-party config (origin allow-list + rpId) from state.
	WebAuthnParams WebAuthnParamSource
	// VaultReader backs the gov-param guard (total vault balance; arming a uphi deposit burn is refused while non-zero).
	VaultReader VaultReader
	// IdentityStatus backs the DID-lifecycle guard (reject a signer controlling a non-ACTIVE DID; no-DID passes).
	IdentityStatus IdentityStatusSource
	// UVPolicy sources the governed stepped User-Verification policy (which messages are sensitive).
	UVPolicy UVPolicySource
}

// NewAnteHandler builds the chain of Phi AnteHandler decorators (order is consensus-critical).
func NewAnteHandler(o HandlerOptions) (sdk.AnteHandler, error) {
	if o.AccountKeeper == nil {
		return nil, errors.New("account keeper is required for ante builder")
	}
	if o.BankKeeper == nil {
		return nil, errors.New("bank keeper is required for ante builder")
	}
	if o.CoinKeeper == nil {
		return nil, errors.New("coin keeper is required for ante builder")
	}
	if o.SignModeHandler == nil {
		return nil, errors.New("sign mode handler is required for ante builder")
	}
	if o.VaultReader == nil {
		return nil, errors.New("vault reader is required for ante builder (gov-param guard)")
	}
	if o.WebAuthnParams == nil {
		return nil, errors.New("webauthn param source is required for ante builder")
	}
	if o.IdentityStatus == nil {
		return nil, errors.New("identity status source is required for ante builder (DID lifecycle guard)")
	}
	if o.UVPolicy == nil {
		return nil, errors.New("uv policy source is required for ante builder (stepped User-Verification)")
	}

	decorators := []sdk.AnteDecorator{
		ante.NewSetUpContextDecorator(),
		NewMaxGasDecorator(), // enforce the per-tx gas ceiling before any gas-consuming decorator
		ante.NewExtensionOptionsDecorator(nil),
		ante.NewValidateBasicDecorator(),
		ante.NewTxTimeoutHeightDecorator(),
		ante.NewValidateMemoDecorator(o.AccountKeeper),
		ante.NewConsumeGasForTxSizeDecorator(o.AccountKeeper),
		NewRejectUnsafeGovParamsDecorator(o.VaultReader),                                    // refuse arming a uphi deposit burn while vaults are non-zero
		NewFixedFeeDecorator(o.AccountKeeper, o.BankKeeper, o.FeegrantKeeper, o.CoinKeeper), // fixed per-message fee + feegrant
		ante.NewSetPubKeyDecorator(o.AccountKeeper),
		ante.NewValidateSigCountDecorator(o.AccountKeeper),
		ante.NewSigGasConsumeDecorator(o.AccountKeeper, PhiSigVerificationGasConsumer), // secp256r1 + secp256k1 verification gas
		// consensus-critical: forked SDK sigverify routing WebAuthn envelopes to phi-crypto, k1/r1 upstream.
		NewPhiSigVerificationDecorator(o.AccountKeeper, o.SignModeHandler, o.Verifier, o.WebAuthnParams, o.UVPolicy),
		// DID lifecycle guard: reject a signer controlling a suspended/revoked DID (no-DID passes).
		NewIdentityStatusGuard(o.IdentityStatus),
		ante.NewIncrementSequenceDecorator(o.AccountKeeper),
	}
	return sdk.ChainAnteDecorators(decorators...), nil
}
