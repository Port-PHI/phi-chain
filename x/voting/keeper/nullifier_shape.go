//go:build !voting_snark

// SPDX-License-Identifier: Apache-2.0

package keeper

func checkNullifierShape([]byte) error { return nil }
