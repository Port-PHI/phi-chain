//go:build phicrypto_cgo

// SPDX-License-Identifier: Apache-2.0

package keeper_test

import "github.com/Port-PHI/phi-chain/phicrypto"

func seedVerifier() phicrypto.Verifier { return phicrypto.Default() }
