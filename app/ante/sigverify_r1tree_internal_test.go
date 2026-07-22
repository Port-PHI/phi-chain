// SPDX-License-Identifier: Apache-2.0

package ante

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	kmultisig "github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256r1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/stretchr/testify/require"
)

// pubKeyContainsR1 must detect a secp256r1 leaf anywhere in a nested key tree.
func TestPubKeyContainsR1(t *testing.T) {
	r1Priv, err := secp256r1.GenPrivKey()
	require.NoError(t, err)
	r1 := r1Priv.PubKey()
	k1 := secp256k1.GenPrivKey().PubKey()
	ed := ed25519.GenPrivKey().PubKey()

	cases := []struct {
		name string
		pk   cryptotypes.PubKey
		want bool
	}{
		{"single secp256r1", r1, true},
		{"single secp256k1", k1, false},
		{"single ed25519", ed, false},
		{"multisig with r1 leaf", kmultisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{k1, r1}), true},
		{"multisig all k1", kmultisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{k1, secp256k1.GenPrivKey().PubKey()}), false},
		{
			"nested multisig hiding an r1 leaf",
			kmultisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{
				k1,
				kmultisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{secp256k1.GenPrivKey().PubKey(), r1}),
			}),
			true,
		},
		{
			"nested multisig all k1",
			kmultisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{
				k1,
				kmultisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{secp256k1.GenPrivKey().PubKey()}),
			}),
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, pubKeyContainsR1(tc.pk))
		})
	}
}
