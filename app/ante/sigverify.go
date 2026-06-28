// SPDX-License-Identifier: Apache-2.0

package ante

import (
	errorsmod "cosmossdk.io/errors"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256r1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/crypto/types/multisig"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// PhiSigVerificationGasConsumer is Phi's explicit key-type acceptance policy:
// both the secp256k1 and secp256r1 curves (passkey / Secure Enclave / WebCrypto non-extractable)
// are accepted; ed25519 and unknown types are rejected. This function is consensus-critical.
func PhiSigVerificationGasConsumer(meter storetypes.GasMeter, sig signing.SignatureV2, params authtypes.Params) error {
	switch pubkey := sig.PubKey.(type) {
	case *secp256k1.PubKey:
		meter.ConsumeGas(params.SigVerifyCostSecp256k1, "ante verify: secp256k1")
		return nil

	case *secp256r1.PubKey: // Phi addition: required for non-extractable keys in the browser / Secure Enclave
		meter.ConsumeGas(params.SigVerifyCostSecp256r1(), "ante verify: secp256r1")
		return nil

	case multisig.PubKey:
		// Do NOT delegate to the SDK default consumer — it accepts ed25519 sub-keys, which would
		// let a multisig bypass the Phi key policy. First reject any key in the (possibly nested) tree
		// that is not secp256k1/secp256r1, then consume gas per signed sub-key under the Phi policy.
		if err := assertAllowedKeyTree(pubkey); err != nil {
			return err
		}
		multisignature, ok := sig.Data.(*signing.MultiSignatureData)
		if !ok {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidPubKey, "expected multisignature data, got %T", sig.Data)
		}
		return phiConsumeMultisigGas(meter, multisignature, pubkey, params, sig.Sequence)

	case *ed25519.PubKey:
		return errorsmod.Wrap(sdkerrors.ErrInvalidPubKey, "ed25519 keys are not supported on phi")

	default:
		return errorsmod.Wrapf(sdkerrors.ErrInvalidPubKey, "unsupported key type %T", pubkey)
	}
}

// assertAllowedKeyTree rejects a public key (recursing through every sub-key of a nested multisig via
// GetPubKeys) unless every leaf is secp256k1 or secp256r1. This enforces the Phi key policy across
// the whole multisig structure, not only the sub-keys that happened to sign the current tx.
func assertAllowedKeyTree(pk cryptotypes.PubKey) error {
	switch key := pk.(type) {
	case *secp256k1.PubKey, *secp256r1.PubKey:
		return nil
	case multisig.PubKey:
		for _, sub := range key.GetPubKeys() {
			if err := assertAllowedKeyTree(sub); err != nil {
				return err
			}
		}
		return nil
	default:
		return errorsmod.Wrapf(sdkerrors.ErrInvalidPubKey, "unsupported key type in multisig: %T", pk)
	}
}

// phiConsumeMultisigGas mirrors the SDK's multisig gas accounting but recurses through
// PhiSigVerificationGasConsumer (the Phi policy) for each signed sub-key, so ed25519 sub-keys are
// rejected at every nesting level.
func phiConsumeMultisigGas(meter storetypes.GasMeter, sig *signing.MultiSignatureData, pubkey multisig.PubKey, params authtypes.Params, accSeq uint64) error {
	size := sig.BitArray.Count()
	sigIndex := 0
	for i := 0; i < size; i++ {
		if !sig.BitArray.GetIndex(i) {
			continue
		}
		sigV2 := signing.SignatureV2{
			PubKey:   pubkey.GetPubKeys()[i],
			Data:     sig.Signatures[sigIndex],
			Sequence: accSeq,
		}
		if err := PhiSigVerificationGasConsumer(meter, sigV2, params); err != nil {
			return err
		}
		sigIndex++
	}
	return nil
}
