// SPDX-License-Identifier: Apache-2.0

package ante_test

import (
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	kmultisig "github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256r1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	phiante "github.com/Port-PHI/phi-chain/app/ante"
)

// Tests the key-type acceptance policy (consensus-critical): k1 and r1 accepted, ed25519 and unknown rejected.
func TestPhiSigVerificationGasConsumer_KeyAcceptance(t *testing.T) {
	params := authtypes.DefaultParams()

	r1Priv, err := secp256r1.GenPrivKey()
	require.NoError(t, err)

	cases := []struct {
		name   string
		pubKey cryptotypes.PubKey
		ok     bool
	}{
		{"secp256k1 (legacy)", secp256k1.GenPrivKey().PubKey(), true},
		{"secp256r1 (passkey)", r1Priv.PubKey(), true},
		{"ed25519 (rejected)", ed25519.GenPrivKey().PubKey(), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meter := storetypes.NewInfiniteGasMeter()
			sig := signing.SignatureV2{PubKey: tc.pubKey}
			err := phiante.PhiSigVerificationGasConsumer(meter, sig, params)
			if tc.ok {
				require.NoError(t, err, "key should be accepted")
				require.Positive(t, meter.GasConsumed(), "verification gas should be consumed")
			} else {
				require.Error(t, err, "key should be rejected")
			}
		})
	}
}

// A multisig must not smuggle an ed25519 sub-key past the key policy. Every key in the
// (possibly nested) tree must be secp256k1/secp256r1, even sub-keys that did not sign this tx.
func TestPhiSigVerificationGasConsumer_MultisigKeyPolicy(t *testing.T) {
	params := authtypes.DefaultParams()
	r1Priv, err := secp256r1.GenPrivKey()
	require.NoError(t, err)
	k1, r1 := secp256k1.GenPrivKey().PubKey(), r1Priv.PubKey()
	edKey := ed25519.GenPrivKey().PubKey()

	// An all-{k1,r1} 1-of-2 multisig is accepted (gas consumed for the signed sub-keys).
	t.Run("all allowed sub-keys accepted", func(t *testing.T) {
		ms := kmultisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{k1, r1})
		bits := cryptotypes.NewCompactBitArray(2)
		bits.SetIndex(0, true)
		bits.SetIndex(1, true)
		data := &signing.MultiSignatureData{
			BitArray:   bits,
			Signatures: []signing.SignatureData{&signing.SingleSignatureData{}, &signing.SingleSignatureData{}},
		}
		meter := storetypes.NewInfiniteGasMeter()
		require.NoError(t, phiante.PhiSigVerificationGasConsumer(meter, signing.SignatureV2{PubKey: ms, Data: data}, params))
		require.Positive(t, meter.GasConsumed())
	})

	// A multisig that merely CONTAINS an ed25519 sub-key is rejected (the policy covers the whole
	// tree, so no signature data is even required to reach the rejection).
	t.Run("ed25519 sub-key rejected", func(t *testing.T) {
		ms := kmultisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{k1, edKey})
		meter := storetypes.NewInfiniteGasMeter()
		require.Error(t, phiante.PhiSigVerificationGasConsumer(meter, signing.SignatureV2{PubKey: ms}, params))
	})

	// A nested multisig hiding an ed25519 key one level down is also rejected.
	t.Run("nested ed25519 sub-key rejected", func(t *testing.T) {
		inner := kmultisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{edKey})
		outer := kmultisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{k1, inner})
		meter := storetypes.NewInfiniteGasMeter()
		require.Error(t, phiante.PhiSigVerificationGasConsumer(meter, signing.SignatureV2{PubKey: outer}, params))
	})
}
