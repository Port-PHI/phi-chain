// SPDX-License-Identifier: Apache-2.0

package phicrypto

import (
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"

	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// DIDMethodPrefix is the canonical PHI DID method prefix.
const DIDMethodPrefix = "did:phi:"

// ErrInvalidDIDKey is returned when the public key is not a valid point on the curve (raw bytes are never hashed into the id namespace; fail-closed).
var ErrInvalidDIDKey = errors.New("phicrypto: public key is not a valid point on the curve")

// DeriveDID reproduces phi-crypto's canonical `did:phi` derivation (src/did.rs did_from_public) byte-for-byte: did:phi:<hex(SHA-256(curve_tag ‖ canonical_sec1(pubKey)))>, tag 0x01=k1 (compressed SEC1) / 0x02=r1 (uncompressed).
func DeriveDID(curve Curve, pubKey []byte) (string, error) {
	var (
		tag       byte
		canonical []byte
	)
	switch curve {
	case Secp256r1:
		tag = 0x02
		x, y, err := parseP256(pubKey)
		if err != nil {
			return "", err
		}
		canonical = elliptic.Marshal(elliptic.P256(), x, y) // uncompressed SEC1 (65B)
	case Secp256k1:
		tag = 0x01
		pk, err := secp256k1.ParsePubKey(pubKey)
		if err != nil {
			return "", ErrInvalidDIDKey
		}
		canonical = pk.SerializeCompressed() // compressed SEC1 (33B)
	default:
		return "", ErrInvalidDIDKey
	}

	h := sha256.New()
	h.Write([]byte{tag})
	h.Write(canonical)
	digest := h.Sum(nil)
	return DIDMethodPrefix + hex.EncodeToString(digest), nil
}

func parseP256(b []byte) (x, y *big.Int, err error) {
	if len(b) == 0 {
		return nil, nil, ErrInvalidDIDKey
	}
	switch b[0] {
	case 0x04:
		x, y = elliptic.Unmarshal(elliptic.P256(), b)
	case 0x02, 0x03:
		x, y = elliptic.UnmarshalCompressed(elliptic.P256(), b)
	default:
		return nil, nil, ErrInvalidDIDKey
	}
	if x == nil {
		return nil, nil, ErrInvalidDIDKey
	}
	return x, y, nil
}
