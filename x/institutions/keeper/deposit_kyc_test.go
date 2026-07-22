// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/phicrypto"
	"github.com/Port-PHI/phi-chain/x/institutions/keeper"
	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

func setupWithVerifier(t *testing.T, v phicrypto.Verifier) fixture {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_dep"))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	bank := newFakeBank()
	authority := sdk.AccAddress([]byte("gov_authority_______")).String()
	k := keeper.NewKeeper(cdc, key, authority, bank, fakeIdentity{}, fakeCoin{}, v)
	oper := sdk.AccAddress([]byte("operator____________"))
	require.NoError(t, k.SetParams(testCtx.Ctx, types.Params{Operator: oper.String(), PhiToToman: 100_000, RedeemFloorPerTx: "100"}))
	return fixture{
		ctx: testCtx.Ctx, k: k, msg: keeper.NewMsgServerImpl(k), bank: bank,
		oper: oper, admin: oper, compliance: sdk.AccAddress([]byte("compliance-officer__")),
		holder: sdk.AccAddress([]byte("holder______________")), authority: authority,
	}
}

func depKey() []byte { return bytes.Repeat([]byte{0x02}, 33) }

func framedV2(parts ...[]byte) []byte {
	var out []byte
	var l [4]byte
	for _, p := range parts {
		binary.BigEndian.PutUint32(l[:], uint32(len(p)))
		out = append(out, l[:]...)
		out = append(out, p...)
	}
	return out
}

// Once a deposit key is registered, mint requires a deposit_proof the verifier accepts.
func TestDepositProof_RequiredOnceKeyRegistered(t *testing.T) {
	var seenMsg []byte
	verifier := phicrypto.Fake{SignatureFn: func(curve phicrypto.Curve, pk, msg, sig []byte) bool {
		seenMsg = append([]byte(nil), msg...)
		return curve == phicrypto.Secp256r1 && bytes.Equal(sig, []byte("good-sig"))
	}}
	f := setupWithVerifier(t, verifier)
	f.registerAndAttest(t, "bank-a", 1000)

	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "100", DepositRef: "dep-0",
	})
	require.NoError(t, err)

	res, err := f.msg.SetInstitutionDepositKey(f.ctx, &types.MsgSetInstitutionDepositKey{
		Signer: f.admin.String(), Institution: "bank-a", DepositPubkey: depKey(),
	})
	require.NoError(t, err)
	require.True(t, res.Executed)

	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(),
		AmountToman: "100", DepositRef: "dep-1", DepositProof: []byte("bad-sig"),
	})
	require.ErrorIs(t, err, types.ErrInvalidDepositProof)

	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(),
		AmountToman: "100", DepositRef: "dep-2", DepositProof: []byte("good-sig"),
	})
	require.NoError(t, err)

	wantMsg := framedV2(
		[]byte("phi-deposit-attestation-v2"),
		[]byte("bank-a"), []byte(f.holder.String()), []byte("100"), []byte("dep-2"),
	)
	require.Equal(t, wantMsg, seenMsg)
}

// An fx institution's deposit message includes the fx provenance fields.
func TestDepositProof_FxProvenanceInMessage(t *testing.T) {
	var seenMsg []byte
	verifier := phicrypto.Fake{SignatureFn: func(_ phicrypto.Curve, _, msg, _ []byte) bool {
		seenMsg = append([]byte(nil), msg...)
		return true
	}}
	f := setupWithVerifier(t, verifier)
	f.registerFxAndAttest(t, "exchange-1", 1000)
	_, err := f.msg.SetInstitutionDepositKey(f.ctx, &types.MsgSetInstitutionDepositKey{
		Signer: f.admin.String(), Institution: "exchange-1", DepositPubkey: depKey(),
	})
	require.NoError(t, err)

	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "exchange-1", Recipient: f.holder.String(),
		AmountToman: "100", DepositRef: "dep-1", DepositProof: []byte("sig"),
		FxCurrency: "BTC", FxAmount: "3", FxTxRef: "0xabc",
	})
	require.NoError(t, err)

	wantMsg := framedV2(
		[]byte("phi-deposit-attestation-v2"),
		[]byte("exchange-1"), []byte(f.holder.String()), []byte("100"), []byte("dep-1"),
		[]byte("BTC"), []byte("3"), []byte("0xabc"),
	)
	require.Equal(t, wantMsg, seenMsg)
}

// A fail-closed verifier rejects every deposit proof once a key is set (RejectAll models the default build).
func TestDepositProof_FailsClosedByDefault(t *testing.T) {
	f := setupWithVerifier(t, phicrypto.RejectAll())
	f.registerAndAttest(t, "bank-a", 1000)
	_, err := f.msg.SetInstitutionDepositKey(f.ctx, &types.MsgSetInstitutionDepositKey{
		Signer: f.admin.String(), Institution: "bank-a", DepositPubkey: depKey(),
	})
	require.NoError(t, err)
	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(),
		AmountToman: "100", DepositRef: "d", DepositProof: []byte("anything"),
	})
	require.ErrorIs(t, err, types.ErrInvalidDepositProof)
}

// The KYC-tier daily limit is enforced on mint when the institution configures the holder's tier.
func TestKycTierLimit_EnforcedOnMint(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 1_000_000)
	_, err := f.msg.UpdateInstitutionParams(f.ctx, &types.MsgUpdateInstitutionParams{
		Signer: f.admin.String(), Institution: "bank-a",
		Params: types.InstitutionParams{
			Caps: types.Caps{},
			KycTierLimits: []types.KycTierLimit{
				{Tier: 1, DailyLimitToman: "1000"},
			},
		},
	})
	require.NoError(t, err)

	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "600", KycTier: 1, DepositRef: "dep-1",
	})
	require.NoError(t, err)
	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "500", KycTier: 1, DepositRef: "dep-2",
	})
	require.ErrorIs(t, err, types.ErrKycTierExceeded)

	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "500", KycTier: 2, DepositRef: "dep-3",
	})
	require.ErrorIs(t, err, types.ErrKycTierExceeded,
		"an unconfigured tier must fall to the strictest configured limit, not escape the cap")
}
