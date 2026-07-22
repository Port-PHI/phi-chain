//go:build reauth && !phicrypto_cgo

// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"math/big"

	"github.com/Port-PHI/phi-chain/phicrypto"
)

func reauthVerifier() phicrypto.Verifier {
	f := phicrypto.RejectAll() // anything not implemented here fails closed
	f.SignatureFn = func(curve phicrypto.Curve, publicKey, msg, sig []byte) bool {
		if curve != phicrypto.Secp256r1 || len(sig) != 64 {
			return false
		}
		pub, ok := parseP256Test(publicKey)
		if !ok {
			return false
		}
		r := new(big.Int).SetBytes(sig[:32])
		s := new(big.Int).SetBytes(sig[32:])
		halfN := new(big.Int).Rsh(elliptic.P256().Params().N, 1)
		if s.Cmp(halfN) > 0 {
			return false
		}
		h := sha256.Sum256(msg)
		return ecdsa.Verify(pub, h[:], r, s)
	}
	return f
}

func parseP256Test(b []byte) (*ecdsa.PublicKey, bool) {
	curve := elliptic.P256()
	var x, y *big.Int
	switch {
	case len(b) == 33:
		x, y = elliptic.UnmarshalCompressed(curve, b)
	case len(b) == 65:
		x, y = elliptic.Unmarshal(curve, b) //nolint:staticcheck // SEC1 uncompressed: the stdlib's only parser
	default:
		return nil, false
	}
	if x == nil || y == nil {
		return nil, false
	}
	return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, true
}
