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

// PhiSigVerificationGasConsumer is Phi's key-type policy (consensus-critical): accept secp256k1+secp256r1, reject ed25519/unknown.
func PhiSigVerificationGasConsumer(meter storetypes.GasMeter, sig signing.SignatureV2, params authtypes.Params) error {
	switch pubkey := sig.PubKey.(type) {
	case *secp256k1.PubKey:
		meter.ConsumeGas(params.SigVerifyCostSecp256k1, "ante verify: secp256k1")
		return nil

	case *secp256r1.PubKey: // required for non-extractable browser / Secure Enclave keys
		meter.ConsumeGas(params.SigVerifyCostSecp256r1(), "ante verify: secp256r1")
		return nil

	case multisig.PubKey:
		// Reject any non-k1/r1 key in the (nested) tree, then consume gas per signed sub-key under the Phi policy.
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

func pubKeyContainsR1(pk cryptotypes.PubKey) bool {
	switch key := pk.(type) {
	case *secp256r1.PubKey:
		return true
	case *secp256k1.PubKey:
		return false
	case multisig.PubKey:
		for _, sub := range key.GetPubKeys() {
			if pubKeyContainsR1(sub) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

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
