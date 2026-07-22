// SPDX-License-Identifier: Apache-2.0

package ante_test

import (
	"testing"

	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256r1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/stretchr/testify/require"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	identitytypes "github.com/Port-PHI/phi-chain/x/identity/types"
)

func (f anteFixture) signedTxMsgs(t *testing.T, priv cryptotypes.PrivKey, acc sdk.AccountI, msgs ...sdk.Msg) sdk.Tx {
	t.Helper()
	txCfg := f.app.TxConfig()
	tb := txCfg.NewTxBuilder()
	require.NoError(t, tb.SetMsgs(msgs...))
	tb.SetGasLimit(200_000)
	require.NoError(t, tb.SetSignatures(signing.SignatureV2{
		PubKey:   priv.PubKey(),
		Data:     &signing.SingleSignatureData{SignMode: signing.SignMode_SIGN_MODE_DIRECT},
		Sequence: acc.GetSequence(),
	}))
	sd := authsigning.SignerData{
		ChainID:       anteChainID,
		AccountNumber: acc.GetAccountNumber(),
		Sequence:      acc.GetSequence(),
		PubKey:        priv.PubKey(),
		Address:       acc.GetAddress().String(),
	}
	sig, err := clienttx.SignWithPrivKey(f.ctx, signing.SignMode_SIGN_MODE_DIRECT, sd, tb, priv, txCfg, acc.GetSequence())
	require.NoError(t, err)
	require.NoError(t, tb.SetSignatures(sig))
	return tb.GetTx()
}

func sensitiveGuardianMsg(acc sdk.AccountI) sdk.Msg {
	return &identitytypes.MsgSetGuardians{
		Controller:  acc.GetAddress().String(),
		Did:         "did:phi:aa",
		Commitments: [][]byte{make([]byte, 32)},
		Threshold:   1,
	}
}

// An r1 account key on a UV-sensitive message with a raw (non-WebAuthn) signature must be rejected: no UV gesture.
func TestRouter_SteppedUV_RawR1OnSensitiveRejected(t *testing.T) {
	f := newAnteFixture(t)
	r1, err := secp256r1.GenPrivKey()
	require.NoError(t, err)
	acc := f.mkAccount(t, r1.PubKey(), 0)

	tx := f.signedTxMsgs(t, r1, acc, sensitiveGuardianMsg(acc))
	dec := f.phiDecoratorUV(uvAwareVerifier(), uvWebAuthnParams(), sensitiveUVPolicy())

	_, err = f.run(dec, tx)
	require.Error(t, err, "an r1 account must not authenticate a UV-sensitive message with a raw (non-WebAuthn) signature")
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
}

// A k1 (self-custody) account is not subject to the WebAuthn-UV policy.
func TestRouter_SteppedUV_K1OnSensitiveUnaffected(t *testing.T) {
	f := newAnteFixture(t)
	k1 := secp256k1.GenPrivKey()
	acc := f.mkAccount(t, k1.PubKey(), 0)

	tx := f.signedTxMsgs(t, k1, acc, sensitiveGuardianMsg(acc))
	dec := f.phiDecoratorUV(uvAwareVerifier(), uvWebAuthnParams(), sensitiveUVPolicy())

	_, err := f.run(dec, tx)
	require.NoError(t, err, "a k1 self-custody account is not subject to the WebAuthn-UV policy")
}

// The raw-r1 guard is scoped to UV-sensitive txs: a raw r1 signature on a non-sensitive message is accepted.
func TestRouter_SteppedUV_RawR1OnNonSensitiveAccepted(t *testing.T) {
	f := newAnteFixture(t)
	r1, err := secp256r1.GenPrivKey()
	require.NoError(t, err)
	acc := f.mkAccount(t, r1.PubKey(), 0)

	tx := f.signedTxMsgs(t, r1, acc, anteMsg(acc)) // MsgSend: not sensitive
	dec := f.phiDecoratorUV(uvAwareVerifier(), uvWebAuthnParams(), sensitiveUVPolicy())

	_, err = f.run(dec, tx)
	require.NoError(t, err, "a raw r1 signature on a non-sensitive message is unaffected by the UV guard")
}

// The uv_large_transfer_uphi threshold triggers raw-r1 treatment: a large raw-r1 transfer is rejected.
func TestRouter_SteppedUV_RawR1LargeTransfer(t *testing.T) {
	transfer := func(acc sdk.AccountI, amount string) sdk.Msg {
		return &cointypes.MsgTransfer{From: acc.GetAddress().String(), To: acc.GetAddress().String(), Amount: amount}
	}
	dec := func(f anteFixture) sdk.AnteDecorator {
		return f.phiDecoratorUV(uvAwareVerifier(), uvWebAuthnParams(), sensitiveUVPolicy())
	}

	t.Run("large raw-r1 transfer rejected (behaves like a sensitive msg)", func(t *testing.T) {
		f := newAnteFixture(t)
		r1, err := secp256r1.GenPrivKey()
		require.NoError(t, err)
		acc := f.mkAccount(t, r1.PubKey(), 0)
		tx := f.signedTxMsgs(t, r1, acc, transfer(acc, "100000000")) // == threshold
		_, err = f.run(dec(f), tx)
		require.Error(t, err, "a large raw-r1 transfer must be rejected like a sensitive msg")
		require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	})

	t.Run("sub-threshold raw-r1 transfer accepted", func(t *testing.T) {
		f := newAnteFixture(t)
		r1, err := secp256r1.GenPrivKey()
		require.NoError(t, err)
		acc := f.mkAccount(t, r1.PubKey(), 0)
		tx := f.signedTxMsgs(t, r1, acc, transfer(acc, "99999999")) // below threshold
		_, err = f.run(dec(f), tx)
		require.NoError(t, err, "a sub-threshold raw-r1 transfer is not sensitive and is accepted")
	})
}

// On every path the guard does not affect, the router accepts/rejects and meters gas exactly as upstream.
func TestRouter_SteppedUV_DifferentialParity_NonAffectedPaths(t *testing.T) {
	k1 := secp256k1.GenPrivKey()
	r1, err := secp256r1.GenPrivKey()
	require.NoError(t, err)

	cases := []struct {
		name string
		priv cryptotypes.PrivKey
		msg  func(sdk.AccountI) sdk.Msg
	}{
		{"k1 + sensitive msg (k1 not subject to UV)", k1, sensitiveGuardianMsg},
		{"k1 + non-sensitive msg", k1, anteMsg},
		{"r1 + non-sensitive msg", r1, anteMsg},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAnteFixture(t)
			acc := f.mkAccount(t, tc.priv.PubKey(), 0)
			tx := f.signedTxMsgs(t, tc.priv, acc, tc.msg(acc))

			gPhi, ePhi := f.run(f.phiDecoratorUV(uvAwareVerifier(), uvWebAuthnParams(), sensitiveUVPolicy()), tx)
			gUp, eUp := f.run(f.upstreamDecorator(), tx)
			require.Equal(t, eUp == nil, ePhi == nil, "accept/reject must match upstream on a non-affected path")
			require.NoError(t, ePhi, "a non-affected path must be accepted")
			require.Equal(t, gUp, gPhi, "gas must equal upstream on a non-affected path")
		})
	}
}
