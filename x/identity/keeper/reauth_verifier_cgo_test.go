//go:build reauth && phicrypto_cgo

// SPDX-License-Identifier: Apache-2.0

package keeper_test

import "github.com/Port-PHI/phi-chain/phicrypto"

func reauthVerifier() phicrypto.Verifier { return phicrypto.Default() }
