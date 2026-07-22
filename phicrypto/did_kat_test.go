// SPDX-License-Identifier: Apache-2.0

package phicrypto_test

import (
	"crypto/elliptic"
	"encoding/hex"
	"testing"

	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/phicrypto"
)

// Cross-language known-answer vectors for canonical did:phi derivation, shared with phi-crypto/tests/did_kat.rs so the Go port and Rust source cannot silently diverge (r1 SEC1 uncompressed 65B, k1 compressed 33B).
const (
	r1PubHex = "040217e617f0b6443928278f96999e69a23a4f2c152bdf6d6cdf66e5b80282d4ed194a7debcb97712d2dda3ca85aa8765a56f45fc758599652f2897c65306e5794"
	r1DID    = "did:phi:1a3d5be062c4389f9a2c4097549d0c7f474a0176238b2f229475bd5842f1663f"

	k1PubHex = "034f355bdcb7cc0af728ef3cceb9615d90684bb5b2ca5f859ab0f0b704075871aa"
	k1DID    = "did:phi:2580a99f69c809435c9ba44f68100a2970fa7fa5923d0fd9ca2befc537334244"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}

func TestDeriveDID_KAT_Secp256r1(t *testing.T) {
	pk := mustHex(t, r1PubHex)
	require.Len(t, pk, 65, "r1 canonical SEC1 is uncompressed")
	did, err := phicrypto.DeriveDID(phicrypto.Secp256r1, pk)
	require.NoError(t, err)
	require.Equal(t, r1DID, did)
}

func TestDeriveDID_KAT_Secp256k1(t *testing.T) {
	pk := mustHex(t, k1PubHex)
	require.Len(t, pk, 33, "k1 canonical SEC1 is compressed")
	did, err := phicrypto.DeriveDID(phicrypto.Secp256k1, pk)
	require.NoError(t, err)
	require.Equal(t, k1DID, did)
}

// Canonicalization: the same r1 key in compressed SEC1 yields the identical DID.
func TestDeriveDID_Secp256r1_CompressedInput_SameDID(t *testing.T) {
	pk := mustHex(t, r1PubHex)
	x, y := elliptic.Unmarshal(elliptic.P256(), pk)
	require.NotNil(t, x)
	compressed := elliptic.MarshalCompressed(elliptic.P256(), x, y)
	require.Len(t, compressed, 33)

	did, err := phicrypto.DeriveDID(phicrypto.Secp256r1, compressed)
	require.NoError(t, err)
	require.Equal(t, r1DID, did, "compressed and uncompressed r1 inputs must map to one DID")
}

// Canonicalization: the same k1 key uncompressed yields the identical DID.
func TestDeriveDID_Secp256k1_UncompressedInput_SameDID(t *testing.T) {
	pk, err := secp256k1.ParsePubKey(mustHex(t, k1PubHex))
	require.NoError(t, err)
	uncompressed := pk.SerializeUncompressed()
	require.Len(t, uncompressed, 65)

	did, err := phicrypto.DeriveDID(phicrypto.Secp256k1, uncompressed)
	require.NoError(t, err)
	require.Equal(t, k1DID, did, "compressed and uncompressed k1 inputs must map to one DID")
}

func TestDeriveDID_RejectsInvalidKey(t *testing.T) {
	_, err := phicrypto.DeriveDID(phicrypto.Secp256r1, []byte("not-a-point"))
	require.ErrorIs(t, err, phicrypto.ErrInvalidDIDKey)
	_, err = phicrypto.DeriveDID(phicrypto.Secp256k1, []byte("not-a-point"))
	require.ErrorIs(t, err, phicrypto.ErrInvalidDIDKey)
	_, err = phicrypto.DeriveDID(phicrypto.Secp256r1, nil)
	require.ErrorIs(t, err, phicrypto.ErrInvalidDIDKey)
	bad := append([]byte{0x02}, bytesRepeat(0xFF, 32)...)
	_, err = phicrypto.DeriveDID(phicrypto.Secp256r1, bad)
	require.ErrorIs(t, err, phicrypto.ErrInvalidDIDKey)
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}
