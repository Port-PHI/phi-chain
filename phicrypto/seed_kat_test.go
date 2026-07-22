// SPDX-License-Identifier: Apache-2.0

package phicrypto_test

import (
	"encoding/hex"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	bip39 "github.com/cosmos/go-bip39"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/phicrypto"
)

// Cross-language known-answer test for the opt-in k1 self-custody path: phi-crypto's Rust seed module and the Cosmos SDK must derive the identical key from one BIP-39 phrase, else a valid phrase would silently control a different identity.
const (
	katMnemonic  = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"
	katSecretHex = "8088c2ed2149c34f6d6533b774da4e1692eb5cb426fdbaef6898eeda489630b7"
	katPubKeyHex = "02ba66a84cf7839af172a13e7fc9f5e7008cb8bca1585f8f3bafb3039eda3c1fdd"
	katDID       = "did:phi:17010867b1779053627535b78920d69733c4ef4725ad72c78948f1b14387472c"
	katPath      = "m/44'/118'/0'/0/0"
)

func deriveK1FromMnemonic(t *testing.T, mnemonic, passphrase, path string) (secret []byte, pubKey []byte) {
	t.Helper()
	seed, err := bip39.NewSeedWithErrorChecking(mnemonic, passphrase)
	require.NoError(t, err)
	master, ch := hd.ComputeMastersFromSeed(seed)
	priv, err := hd.DerivePrivateKeyForPath(master, ch, path)
	require.NoError(t, err)
	pk := &secp256k1.PrivKey{Key: priv}
	return priv, pk.PubKey().Bytes()
}

// The Rust module and the Cosmos SDK derive the same key from one phrase, yielding the canonical k1 DID.
func TestSeedKAT_RustAndCosmosSDKDeriveTheSameK1Identity(t *testing.T) {
	secret, pubKey := deriveK1FromMnemonic(t, katMnemonic, "", katPath)

	require.Equal(t, katSecretHex, hex.EncodeToString(secret),
		"the SDK's derivation must equal phi-crypto's Rust seed module (see src/seed.rs)")
	require.Equal(t, katPubKeyHex, hex.EncodeToString(pubKey), "compressed SEC1 public key")

	did, err := phicrypto.DeriveDID(phicrypto.Secp256k1, pubKey)
	require.NoError(t, err)
	require.Equal(t, katDID, did, "canonical k1 did:phi")
}

// Recovery property: re-deriving from the same phrase reproduces the identical key and DID (no on-chain recovery needed).
func TestSeedKAT_ReDerivationIsDeterministic(t *testing.T) {
	first, firstPub := deriveK1FromMnemonic(t, katMnemonic, "", katPath)
	afterDeviceLoss, afterPub := deriveK1FromMnemonic(t, katMnemonic, "", katPath)
	require.Equal(t, first, afterDeviceLoss, "same phrase → same key")
	require.Equal(t, firstPub, afterPub)

	_, withPassphrase := deriveK1FromMnemonic(t, katMnemonic, "TREZOR", katPath)
	require.NotEqual(t, firstPub, withPassphrase)

	entropy := make([]byte, 32)
	entropy[31] = 1
	otherMnemonic, err := bip39.NewMnemonic(entropy)
	require.NoError(t, err)
	require.NotEqual(t, katMnemonic, otherMnemonic)

	_, otherPub := deriveK1FromMnemonic(t, otherMnemonic, "", katPath)
	require.NotEqual(t, firstPub, otherPub, "a wrong phrase derives a different identity")
}

// The path binds the key: the same phrase one index over derives a different identity.
func TestSeedKAT_PathBindsTheIdentity(t *testing.T) {
	_, atPinnedPath := deriveK1FromMnemonic(t, katMnemonic, "", katPath)
	_, atOtherIndex := deriveK1FromMnemonic(t, katMnemonic, "", "m/44'/118'/0'/0/1")
	_, atOtherAccount := deriveK1FromMnemonic(t, katMnemonic, "", "m/44'/118'/1'/0/0")

	require.NotEqual(t, atPinnedPath, atOtherIndex)
	require.NotEqual(t, atPinnedPath, atOtherAccount)

	pinnedDID, err := phicrypto.DeriveDID(phicrypto.Secp256k1, atPinnedPath)
	require.NoError(t, err)
	otherDID, err := phicrypto.DeriveDID(phicrypto.Secp256k1, atOtherIndex)
	require.NoError(t, err)
	require.NotEqual(t, pinnedDID, otherDID, "a different path is a different identity")
}
