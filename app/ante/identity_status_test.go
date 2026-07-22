// SPDX-License-Identifier: Apache-2.0

package ante_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/stretchr/testify/require"

	phiante "github.com/Port-PHI/phi-chain/app/ante"
)

type fakeStatus struct{ nonActive map[string]bool }

func (f fakeStatus) HasNonActiveDID(_ sdk.Context, controller string) bool {
	return f.nonActive[controller]
}

type signersTx struct {
	authsigning.SigVerifiableTx
	signers [][]byte
}

func (t signersTx) GetSigners() ([][]byte, error) { return t.signers, nil }

// The identity status guard rejects a tx whose signer controls a suspended or revoked DID (the check is message-agnostic, so it blocks both transfers and votes), passes an account with no DID, is skipped on simulate and ReCheckTx, and rejects when ANY of several signers is non-active.
func TestIdentityStatusGuard(t *testing.T) {
	suspended := sdk.AccAddress([]byte("suspended-human_____"))
	revoked := sdk.AccAddress([]byte("revoked-human_______"))
	operator := sdk.AccAddress([]byte("institution-operator"))
	src := fakeStatus{nonActive: map[string]bool{
		suspended.String(): true,
		revoked.String():   true,
	}}

	run := func(simulate, recheck bool, signers ...sdk.AccAddress) error {
		g := phiante.NewIdentityStatusGuard(src)
		addrs := make([][]byte, len(signers))
		for i, a := range signers {
			addrs[i] = a.Bytes()
		}
		ctx := sdk.Context{}.WithIsReCheckTx(recheck)
		noop := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }
		_, err := g.AnteHandle(ctx, signersTx{signers: addrs}, simulate, noop)
		return err
	}

	require.Error(t, run(false, false, suspended))
	require.Error(t, run(false, false, revoked))
	require.NoError(t, run(false, false, operator))
	require.Error(t, run(false, false, operator, suspended))
	require.NoError(t, run(false, false, operator, operator))
	require.NoError(t, run(true, false, suspended))
	require.NoError(t, run(false, true, suspended))
}
