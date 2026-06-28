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
	// WebAuthnParams sources the governed relying-party config (origin allow-list + rpId) from state
	// The former compile-time origin/rpId constants are gone.
	WebAuthnParams WebAuthnParamSource
	// VaultReader backs the gov-param guard: it reports the total institution vault balance so a gov param
	// change that would arm a uphi deposit burn is refused while the peg is live.
	VaultReader VaultReader
}

// NewAnteHandler builds the chain of Phi AnteHandler decorators.
//
// Order is critical (each decorator builds on the previous result):
//  1. WebAuthnDecorator (consensus-critical; pass-through until live WebAuthn enforcement is wired)
//  2. fixed per-message fee + feegrant (instead of gas-price)
//  3. secp256r1 key acceptance alongside secp256k1 (PhiSigVerificationGasConsumer)
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

	decorators := []sdk.AnteDecorator{
		ante.NewSetUpContextDecorator(),
		NewMaxGasDecorator(), // enforce the per-tx gas ceiling before any gas-consuming decorator
		ante.NewExtensionOptionsDecorator(nil),
		ante.NewValidateBasicDecorator(),
		ante.NewTxTimeoutHeightDecorator(),
		ante.NewValidateMemoDecorator(o.AccountKeeper),
		ante.NewConsumeGasForTxSizeDecorator(o.AccountKeeper),
		NewRejectUnsafeGovParamsDecorator(o.VaultReader),                                    // refuse arming a uphi deposit burn while vaults are non-zero
		NewWebAuthnDecorator(o.Verifier, o.WebAuthnParams),                                  // step 2 — consensus-critical (gated until mainnet; reads governed relying-party params)
		NewFixedFeeDecorator(o.AccountKeeper, o.BankKeeper, o.FeegrantKeeper, o.CoinKeeper), // step 3 + feegrant (step 4)
		ante.NewSetPubKeyDecorator(o.AccountKeeper),
		ante.NewValidateSigCountDecorator(o.AccountKeeper),
		ante.NewSigGasConsumeDecorator(o.AccountKeeper, PhiSigVerificationGasConsumer), // step 1 — secp256r1 + secp256k1
		ante.NewSigVerificationDecorator(o.AccountKeeper, o.SignModeHandler),
		ante.NewIncrementSequenceDecorator(o.AccountKeeper),
	}
	return sdk.ChainAnteDecorators(decorators...), nil
}
