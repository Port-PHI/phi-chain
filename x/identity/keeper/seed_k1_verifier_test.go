//go:build !phicrypto_cgo

// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"math/big"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"

	"github.com/Port-PHI/phi-chain/phicrypto"
)

func seedVerifier() phicrypto.Verifier {
	f := phicrypto.RejectAll() // anything not implemented here fails closed
	f.SignatureFn = func(curve phicrypto.Curve, publicKey, msg, sig []byte) bool {
		if len(sig) != 64 {
			return false
		}
		switch curve {
		case phicrypto.Secp256k1:
			pk := secp256k1.PubKey{Key: publicKey}
			return pk.VerifySignature(msg, sig)
		case phicrypto.Secp256r1:
			return verifyP256Raw(publicKey, msg, sig)
		default:
			return false
		}
	}
	return f
}

func verifyP256Raw(publicKey, msg, sig []byte) bool {
	curve := elliptic.P256()
	var x, y *big.Int
	switch len(publicKey) {
	case 33:
		x, y = elliptic.UnmarshalCompressed(curve, publicKey)
	case 65:
		x, y = elliptic.Unmarshal(curve, publicKey) //nolint:staticcheck // SEC1 uncompressed: the stdlib's only parser
	default:
		return false
	}
	if x == nil || y == nil {
		return false
	}
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])
	if s.Cmp(new(big.Int).Rsh(curve.Params().N, 1)) > 0 {
		return false
	}
	h := sha256.Sum256(msg)
	return ecdsa.Verify(&ecdsa.PublicKey{Curve: curve, X: x, Y: y}, h[:], r, s)
}
