// SPDX-License-Identifier: Apache-2.0

package ante_test

import (
	"testing"

	kmultisig "github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256r1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/crypto/types/multisig"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/stretchr/testify/require"
)

func (f anteFixture) mkAccountKeyedAt(t *testing.T, addrSource, stored cryptotypes.PubKey, seq uint64) sdk.AccountI {
	t.Helper()
	addr := sdk.AccAddress(addrSource.Address())
	acc := f.app.AccountKeeper.NewAccountWithAddress(f.ctx, addr)
	require.NoError(t, acc.SetPubKey(stored))
	require.NoError(t, acc.SetSequence(seq))
	f.app.AccountKeeper.SetAccount(f.ctx, acc)
	return f.app.AccountKeeper.GetAccount(f.ctx, addr)
}

func (f anteFixture) multisigTxUnsigned(t *testing.T, msPub *kmultisig.LegacyAminoPubKey, acc sdk.AccountI, msgs ...sdk.Msg) sdk.Tx {
	t.Helper()
	tb := f.app.TxConfig().NewTxBuilder()
	require.NoError(t, tb.SetMsgs(msgs...))
	tb.SetGasLimit(300_000)
	require.NoError(t, tb.SetSignatures(signing.SignatureV2{
		PubKey:   msPub,
		Data:     multisig.NewMultisig(len(msPub.GetPubKeys())),
		Sequence: acc.GetSequence(),
	}))
	return tb.GetTx()
}

// A multisig account with a secp256r1 leaf performing a UV-sensitive message must be REJECTED.
func TestRouter_SteppedUV_R1LeafMultisigOnSensitiveRejected(t *testing.T) {
	f := newAnteFixture(t)
	k1 := secp256k1.GenPrivKey()
	r1, err := secp256r1.GenPrivKey()
	require.NoError(t, err)

	msPub := kmultisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{k1.PubKey(), r1.PubKey()})
	acc := f.mkAccountKeyedAt(t, secp256k1.GenPrivKey().PubKey(), msPub, 0)

	tx := f.multisigTxUnsigned(t, msPub, acc, sensitiveGuardianMsg(acc))
	dec := f.phiDecoratorUV(uvAwareVerifier(), uvWebAuthnParams(), sensitiveUVPolicy())

	_, err = f.run(dec, tx)
	require.Error(t, err, "a UV-sensitive tx from an r1-backed multisig must be rejected")
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
}

// A nested multisig hiding the secp256r1 leaf one level down is caught too: the guard descends the whole key tree.
func TestRouter_SteppedUV_NestedR1LeafMultisigOnSensitiveRejected(t *testing.T) {
	f := newAnteFixture(t)
	k1a := secp256k1.GenPrivKey()
	k1b := secp256k1.GenPrivKey()
	r1, err := secp256r1.GenPrivKey()
	require.NoError(t, err)

	inner := kmultisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{k1b.PubKey(), r1.PubKey()})
	outer := kmultisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{k1a.PubKey(), inner})
	acc := f.mkAccountKeyedAt(t, secp256k1.GenPrivKey().PubKey(), outer, 0)

	tx := f.multisigTxUnsigned(t, outer, acc, sensitiveGuardianMsg(acc))
	dec := f.phiDecoratorUV(uvAwareVerifier(), uvWebAuthnParams(), sensitiveUVPolicy())

	_, err = f.run(dec, tx)
	require.Error(t, err, "a UV-sensitive tx from a nested r1-leaf multisig must be rejected")
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
}

// A multisig whose leaves are all secp256k1 (self-custody) carries no passkey/UV concept and is NOT subject to the stepped-UV guard: even under a sensitive policy the router treats it exactly as the upstream SigVerificationDecorator would (same accept/reject, same gas).
func TestRouter_SteppedUV_K1MultisigMatchesUpstream(t *testing.T) {
	f := newAnteFixture(t)
	k1a := secp256k1.GenPrivKey()
	k1b := secp256k1.GenPrivKey()

	msPub := kmultisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{k1a.PubKey(), k1b.PubKey()})
	acc := f.mkAccount(t, msPub, 0)

	tx := f.multisigTxUnsigned(t, msPub, acc, sensitiveGuardianMsg(acc))

	gPhi, ePhi := f.run(f.phiDecoratorUV(uvAwareVerifier(), uvWebAuthnParams(), sensitiveUVPolicy()), tx)
	gUp, eUp := f.run(f.upstreamDecorator(), tx)
	require.Equal(t, eUp == nil, ePhi == nil, "a k1-only multisig must accept/reject exactly as upstream even under a sensitive policy")
	require.Equal(t, gUp, gPhi, "gas must equal upstream on a k1-only multisig")
}
