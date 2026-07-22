// SPDX-License-Identifier: Apache-2.0

package ante_test

import (
	"bytes"
	"os"
	"testing"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256r1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/app"
	phiante "github.com/Port-PHI/phi-chain/app/ante"
	"github.com/Port-PHI/phi-chain/phicrypto"
)

const anteChainID = "phi-ante-test"

// TestMain configures the "phi" bech32 prefix that account address stringification relies on (production does this in the node's PreRun; the app package's own tests do the same).
func TestMain(m *testing.M) {
	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount(app.AccountAddressPrefix, app.AccountAddressPrefix+"pub")
	cfg.SetBech32PrefixForValidator(app.AccountAddressPrefix+"valoper", app.AccountAddressPrefix+"valoperpub")
	cfg.SetBech32PrefixForConsensusNode(app.AccountAddressPrefix+"valcons", app.AccountAddressPrefix+"valconspub")
	os.Exit(m.Run())
}

type anteFixture struct {
	app *app.App
	ctx sdk.Context
}

func newAnteFixture(t *testing.T) anteFixture {
	t.Helper()
	a := app.NewApp(log.NewNopLogger(), dbm.NewMemDB(), nil, true, simtestutil.NewAppOptionsWithFlagHome(t.TempDir()), false)
	ctx := a.NewUncachedContext(false, cmtproto.Header{Height: 1, ChainID: anteChainID})
	return anteFixture{app: a, ctx: ctx}
}

func (f anteFixture) mkAccount(t *testing.T, pub cryptotypes.PubKey, seq uint64) sdk.AccountI {
	t.Helper()
	addr := sdk.AccAddress(pub.Address())
	acc := f.app.AccountKeeper.NewAccountWithAddress(f.ctx, addr)
	require.NoError(t, acc.SetPubKey(pub))
	require.NoError(t, acc.SetSequence(seq))
	f.app.AccountKeeper.SetAccount(f.ctx, acc)
	return f.app.AccountKeeper.GetAccount(f.ctx, addr)
}

func anteMsg(from sdk.AccountI) sdk.Msg {
	addr := from.GetAddress()
	return banktypes.NewMsgSend(addr, addr, sdk.NewCoins(sdk.NewInt64Coin("uphi", 1)))
}

func (f anteFixture) signedTx(t *testing.T, priv cryptotypes.PrivKey, acc sdk.AccountI) sdk.Tx {
	t.Helper()
	txCfg := f.app.TxConfig()
	tb := txCfg.NewTxBuilder()
	require.NoError(t, tb.SetMsgs(anteMsg(acc)))
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

func (f anteFixture) envelopeTx(t *testing.T, r1 cryptotypes.PubKey, acc sdk.AccountI, envelope []byte, mode signing.SignMode) (sdk.Tx, []byte) {
	return f.envelopeTxMsgs(t, r1, acc, envelope, mode, anteMsg(acc))
}

func (f anteFixture) envelopeTxMsgs(t *testing.T, r1 cryptotypes.PubKey, acc sdk.AccountI, envelope []byte, mode signing.SignMode, msgs ...sdk.Msg) (sdk.Tx, []byte) {
	t.Helper()
	txCfg := f.app.TxConfig()
	tb := txCfg.NewTxBuilder()
	require.NoError(t, tb.SetMsgs(msgs...))
	tb.SetGasLimit(200_000)
	require.NoError(t, tb.SetSignatures(signing.SignatureV2{
		PubKey:   r1,
		Data:     &signing.SingleSignatureData{SignMode: mode, Signature: envelope},
		Sequence: acc.GetSequence(),
	}))
	theTx := tb.GetTx()
	sd := authsigning.SignerData{
		ChainID:       anteChainID,
		AccountNumber: acc.GetAccountNumber(),
		Sequence:      acc.GetSequence(),
		PubKey:        r1,
		Address:       acc.GetAddress().String(),
	}
	signBytes, err := authsigning.GetSignBytesAdapter(f.ctx, txCfg.SignModeHandler(), signing.SignMode_SIGN_MODE_DIRECT, sd, theTx)
	require.NoError(t, err)
	return theTx, signBytes
}

func (f anteFixture) run(dec sdk.AnteDecorator, tx sdk.Tx) (uint64, error) {
	ctx := f.ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
	noop := func(c sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return c, nil }
	_, err := dec.AnteHandle(ctx, tx, false, noop)
	return ctx.GasMeter().GasConsumed(), err
}

func (f anteFixture) phiDecorator(v phicrypto.Verifier, params phiante.WebAuthnParamSource) phiante.PhiSigVerificationDecorator {
	return f.phiDecoratorUV(v, params, noUVPolicy())
}

func (f anteFixture) phiDecoratorUV(v phicrypto.Verifier, params phiante.WebAuthnParamSource, uv phiante.UVPolicySource) phiante.PhiSigVerificationDecorator {
	return phiante.NewPhiSigVerificationDecorator(f.app.AccountKeeper, f.app.TxConfig().SignModeHandler(), v, params, uv)
}

func (f anteFixture) upstreamDecorator() authante.SigVerificationDecorator {
	return authante.NewSigVerificationDecorator(f.app.AccountKeeper, f.app.TxConfig().SignModeHandler())
}

func portphiParams() fakeWebAuthnParams {
	return fakeWebAuthnParams{origins: []string{"https://portphi.com"}, rpID: "portphi.com"}
}

// A k1 and an r1 signature verify identically to the upstream SigVerificationDecorator, both for a valid signature (accepted, same gas) and a tampered one (rejected).
func TestPhiSigVerify_DifferentialVsUpstream(t *testing.T) {
	k1 := secp256k1.GenPrivKey()
	r1, err := secp256r1.GenPrivKey()
	require.NoError(t, err)

	for _, tc := range []struct {
		name string
		priv cryptotypes.PrivKey
	}{
		{"secp256k1", k1},
		{"secp256r1", r1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAnteFixture(t)
			acc := f.mkAccount(t, tc.priv.PubKey(), 0)
			phi := f.phiDecorator(phicrypto.AcceptAll(), portphiParams())
			up := f.upstreamDecorator()

			valid := f.signedTx(t, tc.priv, acc)
			gPhi, ePhi := f.run(phi, valid)
			gUp, eUp := f.run(up, valid)
			require.NoError(t, eUp, "upstream must accept a valid %s signature", tc.name)
			require.NoError(t, ePhi, "phi must accept a valid %s signature (regression)", tc.name)
			require.Equal(t, gUp, gPhi, "gas must equal upstream on the valid path")

			bad := f.tamperedTx(t, tc.priv, acc)
			_, ePhiBad := f.run(phi, bad)
			_, eUpBad := f.run(up, bad)
			require.Error(t, eUpBad, "upstream must reject a tampered %s signature", tc.name)
			require.Error(t, ePhiBad, "phi must reject a tampered %s signature", tc.name)
			require.ErrorIs(t, ePhiBad, sdkerrors.ErrUnauthorized)
		})
	}
}

func (f anteFixture) tamperedTx(t *testing.T, priv cryptotypes.PrivKey, acc sdk.AccountI) sdk.Tx {
	t.Helper()
	txCfg := f.app.TxConfig()
	tb := txCfg.NewTxBuilder()
	require.NoError(t, tb.SetMsgs(anteMsg(acc)))
	tb.SetGasLimit(200_000)
	require.NoError(t, tb.SetSignatures(signing.SignatureV2{
		PubKey:   priv.PubKey(),
		Data:     &signing.SingleSignatureData{SignMode: signing.SignMode_SIGN_MODE_DIRECT},
		Sequence: acc.GetSequence(),
	}))
	sd := authsigning.SignerData{ChainID: anteChainID, AccountNumber: acc.GetAccountNumber(), Sequence: acc.GetSequence(), PubKey: priv.PubKey(), Address: acc.GetAddress().String()}
	sig, err := clienttx.SignWithPrivKey(f.ctx, signing.SignMode_SIGN_MODE_DIRECT, sd, tb, priv, txCfg, acc.GetSequence())
	require.NoError(t, err)
	single := sig.Data.(*signing.SingleSignatureData)
	single.Signature[0] ^= 0xFF // flip a byte — still the right length, wrong signature
	require.NoError(t, tb.SetSignatures(sig))
	return tb.GetTx()
}

// A mixed-signer tx (one k1 signature + one passkey envelope) is accepted, and each signer is verified exactly once: the passkey reaches the phi-crypto port exactly once, the k1 stays on the upstream path.
func TestPhiSigVerify_MixedSigner(t *testing.T) {
	f := newAnteFixture(t)
	k1 := secp256k1.GenPrivKey()
	r1, err := secp256r1.GenPrivKey()
	require.NoError(t, err)
	accK1 := f.mkAccount(t, k1.PubKey(), 0)
	accR1 := f.mkAccount(t, r1.PubKey(), 0)

	calls := 0
	fake := phicrypto.Fake{WebAuthnFn: func(a phicrypto.WebAuthnAssertion) bool {
		calls++
		return true
	}}

	tx := f.mixedTx(t, k1, accK1, r1.PubKey(), accR1, sampleEnvelope().Marshal())
	_, err = f.run(f.phiDecorator(fake, portphiParams()), tx)
	require.NoError(t, err, "a k1 + passkey mixed-signer tx must be accepted")
	require.Equal(t, 1, calls, "the passkey signer must reach the port exactly once (no gap, no double-verify)")
}

func (f anteFixture) mixedTx(t *testing.T, k1 cryptotypes.PrivKey, accK1 sdk.AccountI, r1 cryptotypes.PubKey, accR1 sdk.AccountI, envelope []byte) sdk.Tx {
	t.Helper()
	txCfg := f.app.TxConfig()
	tb := txCfg.NewTxBuilder()
	require.NoError(t, tb.SetMsgs(anteMsg(accK1), anteMsg(accR1)))
	tb.SetGasLimit(300_000)
	envSig := signing.SignatureV2{
		PubKey:   r1,
		Data:     &signing.SingleSignatureData{SignMode: signing.SignMode_SIGN_MODE_DIRECT, Signature: envelope},
		Sequence: accR1.GetSequence(),
	}
	require.NoError(t, tb.SetSignatures(
		signing.SignatureV2{PubKey: k1.PubKey(), Data: &signing.SingleSignatureData{SignMode: signing.SignMode_SIGN_MODE_DIRECT}, Sequence: accK1.GetSequence()},
		envSig,
	))
	sd := authsigning.SignerData{ChainID: anteChainID, AccountNumber: accK1.GetAccountNumber(), Sequence: accK1.GetSequence(), PubKey: k1.PubKey(), Address: accK1.GetAddress().String()}
	k1Sig, err := clienttx.SignWithPrivKey(f.ctx, signing.SignMode_SIGN_MODE_DIRECT, sd, tb, k1, txCfg, accK1.GetSequence())
	require.NoError(t, err)
	require.NoError(t, tb.SetSignatures(k1Sig, envSig))
	return tb.GetTx()
}

// Gas parity: the accepted-passkey path consumes exactly the same gas as an accepted raw-r1 path.
func TestPhiSigVerify_GasParity_PasskeyEqualsRawR1(t *testing.T) {
	f := newAnteFixture(t)
	r1, err := secp256r1.GenPrivKey()
	require.NoError(t, err)
	acc := f.mkAccount(t, r1.PubKey(), 0)
	dec := f.phiDecorator(phicrypto.AcceptAll(), portphiParams())

	rawTx := f.signedTx(t, r1, acc)
	gRaw, err := f.run(dec, rawTx)
	require.NoError(t, err)

	envTx, _ := f.envelopeTx(t, r1.PubKey(), acc, sampleEnvelope().Marshal(), signing.SignMode_SIGN_MODE_DIRECT)
	gEnv, err := f.run(dec, envTx)
	require.NoError(t, err)

	require.Equal(t, gRaw, gEnv, "the passkey accept path must consume the same gas as a raw r1 signer (only state access; sig-verify gas is charged earlier)")
}

// A passkey envelope is accepted only under a valid assertion (the port accepts under an allowed origin).
func TestPhiSigVerify_EnvelopeAcceptedUnderValidAssertion(t *testing.T) {
	f := newAnteFixture(t)
	r1, err := secp256r1.GenPrivKey()
	require.NoError(t, err)
	acc := f.mkAccount(t, r1.PubKey(), 0)

	fake := phicrypto.Fake{WebAuthnFn: func(a phicrypto.WebAuthnAssertion) bool {
		return a.Origin == "https://portphi.com" && a.RPID == "portphi.com"
	}}
	tx, _ := f.envelopeTx(t, r1.PubKey(), acc, sampleEnvelope().Marshal(), signing.SignMode_SIGN_MODE_DIRECT)
	_, err = f.run(f.phiDecorator(fake, portphiParams()), tx)
	require.NoError(t, err, "a valid passkey assertion under an allowed origin must be accepted")
}

// The whole tx is rejected (fail-closed) when the assertion is bad.
func TestPhiSigVerify_EnvelopeRejected(t *testing.T) {
	f := newAnteFixture(t)
	r1, err := secp256r1.GenPrivKey()
	require.NoError(t, err)
	acc := f.mkAccount(t, r1.PubKey(), 0)
	env := sampleEnvelope().Marshal()

	_, signBytes := f.envelopeTx(t, r1.PubKey(), acc, env, signing.SignMode_SIGN_MODE_DIRECT)
	boundChallenge := phiante.WebAuthnChallenge(signBytes)

	cases := []struct {
		name     string
		verifier phicrypto.Verifier
		params   phiante.WebAuthnParamSource
		mode     signing.SignMode
		envelope []byte
	}{
		{
			name: "wrong challenge",
			verifier: phicrypto.Fake{WebAuthnFn: func(a phicrypto.WebAuthnAssertion) bool {
				return bytes.Equal(a.Challenge, []byte("some-other-challenge"))
			}},
			params: portphiParams(), mode: signing.SignMode_SIGN_MODE_DIRECT, envelope: env,
		},
		{
			name:     "wrong origin (phishing origin not in allow-list)",
			verifier: phicrypto.Fake{WebAuthnFn: func(a phicrypto.WebAuthnAssertion) bool { return a.Origin == "https://phishing.example" }},
			params:   portphiParams(), mode: signing.SignMode_SIGN_MODE_DIRECT, envelope: env,
		},
		{
			name:     "wrong rpId",
			verifier: phicrypto.Fake{WebAuthnFn: func(a phicrypto.WebAuthnAssertion) bool { return a.RPID == "portphi.com" }},
			params:   fakeWebAuthnParams{origins: []string{"https://portphi.com"}, rpID: "attacker.example"},
			mode:     signing.SignMode_SIGN_MODE_DIRECT, envelope: env,
		},
		{
			name:     "port rejects (missing UP / high-S / malformed clientDataJSON)",
			verifier: phicrypto.RejectAll(),
			params:   portphiParams(), mode: signing.SignMode_SIGN_MODE_DIRECT, envelope: env,
		},
		{
			name:     "non-DIRECT sign mode",
			verifier: phicrypto.AcceptAll(), // would accept, but the DIRECT-only gate rejects first
			params:   portphiParams(), mode: signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON, envelope: env,
		},
		{
			name:     "malformed envelope (magic then truncated)",
			verifier: phicrypto.AcceptAll(),
			params:   portphiParams(), mode: signing.SignMode_SIGN_MODE_DIRECT, envelope: []byte("PWA1\x00\x00"),
		},
	}

	require.Len(t, boundChallenge, 32)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx, _ := f.envelopeTx(t, r1.PubKey(), acc, tc.envelope, tc.mode)
			_, err := f.run(f.phiDecorator(tc.verifier, tc.params), tx)
			require.Error(t, err, "a %s envelope must fail the whole tx (fail-closed)", tc.name)
			require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
		})
	}
}

// Anti-replay: an assertion bound to tx A must not authenticate tx B.
func TestPhiSigVerify_AntiReplay(t *testing.T) {
	f := newAnteFixture(t)
	r1, err := secp256r1.GenPrivKey()
	require.NoError(t, err)
	env := sampleEnvelope().Marshal()

	accA := f.mkAccount(t, r1.PubKey(), 0)
	_, signBytesA := f.envelopeTx(t, r1.PubKey(), accA, env, signing.SignMode_SIGN_MODE_DIRECT)
	challengeA := phiante.WebAuthnChallenge(signBytesA)

	boundToA := phicrypto.Fake{WebAuthnFn: func(a phicrypto.WebAuthnAssertion) bool {
		return bytes.Equal(a.Challenge, challengeA)
	}}
	dec := f.phiDecorator(boundToA, portphiParams())

	txA, _ := f.envelopeTx(t, r1.PubKey(), accA, env, signing.SignMode_SIGN_MODE_DIRECT)
	_, err = f.run(dec, txA)
	require.NoError(t, err, "the assertion authenticates its own tx A")

	accA2 := f.mkAccount(t, r1.PubKey(), 1)
	txB, signBytesB := f.envelopeTx(t, r1.PubKey(), accA2, env, signing.SignMode_SIGN_MODE_DIRECT)
	require.NotEqual(t, signBytesA, signBytesB, "different sequence must change the sign-bytes")
	_, err = f.run(dec, txB)
	require.Error(t, err, "an assertion bound to tx A must not authenticate tx B (anti-replay)")
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
}

// The router forwards the envelope to the port verbatim and binds the challenge to this tx's sign-bytes, passes the compressed SEC1 (33-byte) pubkey, and sources origin/rpId from the governed params.
func TestPhiSigVerify_ForwardsAssertionVerbatim(t *testing.T) {
	f := newAnteFixture(t)
	r1, err := secp256r1.GenPrivKey()
	require.NoError(t, err)
	acc := f.mkAccount(t, r1.PubKey(), 0)
	envelope := sampleEnvelope()

	var got phicrypto.WebAuthnAssertion
	capture := phicrypto.Fake{WebAuthnFn: func(a phicrypto.WebAuthnAssertion) bool { got = a; return true }}

	tx, signBytes := f.envelopeTx(t, r1.PubKey(), acc, envelope.Marshal(), signing.SignMode_SIGN_MODE_DIRECT)
	_, err = f.run(f.phiDecorator(capture, portphiParams()), tx)
	require.NoError(t, err)

	require.Equal(t, envelope.AuthenticatorData, got.AuthenticatorData)
	require.Equal(t, envelope.ClientDataJSON, got.ClientDataJSON)
	require.Equal(t, envelope.Signature, got.Signature)
	require.Equal(t, phiante.WebAuthnChallenge(signBytes), got.Challenge, "challenge must bind THIS tx's sign-bytes")
	require.Equal(t, r1.PubKey().Bytes(), got.PublicKey)
	require.Len(t, got.PublicKey, 33, "secp256r1.PubKey.Bytes() is compressed SEC1 (guardrail 6)")
	require.Equal(t, "https://portphi.com", got.Origin)
	require.Equal(t, "portphi.com", got.RPID)
}

// With the default tagless verifier (phicrypto.Default() == Disabled), every envelope fails closed even though the tx is otherwise well-formed — the fail-safe posture without -tags phicrypto_cgo.
func TestPhiSigVerify_TaglessFailsClosed(t *testing.T) {
	f := newAnteFixture(t)
	r1, err := secp256r1.GenPrivKey()
	require.NoError(t, err)
	acc := f.mkAccount(t, r1.PubKey(), 0)
	tx, _ := f.envelopeTx(t, r1.PubKey(), acc, sampleEnvelope().Marshal(), signing.SignMode_SIGN_MODE_DIRECT)
	_, err = f.run(f.phiDecorator(phicrypto.Default(), portphiParams()), tx)
	require.Error(t, err, "without the cgo verifier every envelope must fail closed")
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
}

// Routing boundary: the WebAuthn path is entered only for a secp256r1 account key AND the envelope magic.
func TestPhiSigVerify_RoutingBoundary_R1Gate(t *testing.T) {
	t.Run("k1 signature with PWA1 magic is not routed to the WebAuthn verifier", func(t *testing.T) {
		f := newAnteFixture(t)
		k1 := secp256k1.GenPrivKey()
		acc := f.mkAccount(t, k1.PubKey(), 0)

		webauthnCalled := false
		fake := phicrypto.Fake{WebAuthnFn: func(phicrypto.WebAuthnAssertion) bool {
			webauthnCalled = true
			return true // would ACCEPT if (wrongly) reached — the assertion below proves it is not
		}}

		magicSig := append([]byte("PWA1"), bytes.Repeat([]byte{0x01}, 60)...)
		require.True(t, phiante.IsWebAuthnEnvelope(magicSig), "precondition: the crafted sig carries the magic")
		tx, _ := f.envelopeTx(t, k1.PubKey(), acc, magicSig, signing.SignMode_SIGN_MODE_DIRECT)

		_, err := f.run(f.phiDecorator(fake, portphiParams()), tx)
		require.False(t, webauthnCalled, "a k1 account key must never reach the WebAuthn verifier, even with the magic prefix")
		require.Error(t, err, "the magic-prefixed k1 signature must be rejected by the standard path")
		require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	})

	t.Run("valid r1 passkey signer still routes and verifies", func(t *testing.T) {
		f := newAnteFixture(t)
		r1, err := secp256r1.GenPrivKey()
		require.NoError(t, err)
		acc := f.mkAccount(t, r1.PubKey(), 0)

		webauthnCalled := false
		fake := phicrypto.Fake{WebAuthnFn: func(phicrypto.WebAuthnAssertion) bool {
			webauthnCalled = true
			return true
		}}
		tx, _ := f.envelopeTx(t, r1.PubKey(), acc, sampleEnvelope().Marshal(), signing.SignMode_SIGN_MODE_DIRECT)

		_, err = f.run(f.phiDecorator(fake, portphiParams()), tx)
		require.True(t, webauthnCalled, "a genuine r1 passkey signer must route to the WebAuthn verifier")
		require.NoError(t, err, "a valid r1 passkey assertion must verify")
	})
}
