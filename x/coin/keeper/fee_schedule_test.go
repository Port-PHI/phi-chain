// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/coin/types"
	credentialstypes "github.com/Port-PHI/phi-chain/x/credentials/types"
)

func agreementWith(creator sdk.AccAddress, n int) *credentialstypes.MsgCreateAgreement {
	dids := make([]string, n)
	for i := range dids {
		dids[i] = fmt.Sprintf("did:phi:signer-%d", i)
	}
	return &credentialstypes.MsgCreateAgreement{
		Creator: creator.String(), Hash: []byte("agreement-hash"), RequiredSigners: dids,
	}
}

// The personal-vault anchor is priced per the schedule: 0.12 PHI, not the flat default fee.
func TestFeeForMsg_AnchorPersonalCosts012Phi(t *testing.T) {
	ctx, k, _, _ := setupCoin(t)
	owner := sdk.AccAddress([]byte("anchor_owner________"))

	anchor := &credentialstypes.MsgAnchorPersonal{Owner: owner.String(), OwnerDid: "did:phi:x"}
	require.Equal(t, math.NewInt(120_000), k.FeeForMsg(ctx, anchor), "0.12 PHI = 120,000 uphi")
}

// The agreement fee is the first CONTENT-dependent fee: base 0.05 PHI, and every required signer FROM THE 6TH onward adds 0.005 PHI.
func TestFeeForMsg_AgreementSurchargeStartsAtTheSixthSigner(t *testing.T) {
	ctx, k, _, _ := setupCoin(t)
	creator := sdk.AccAddress([]byte("agreement_creator___"))

	cases := []struct {
		signers int
		want    int64
		why     string
	}{
		{1, 50_000, "a single signer pays the base fee only"},
		{5, 50_000, "the 5th signer is still free of surcharge"},
		{6, 55_000, "the 6th signer is the FIRST charged: 0.05 + 1 x 0.005"},
		{7, 60_000, "0.05 + 2 x 0.005"},
		{10, 75_000, "0.05 + 5 x 0.005"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%d_signers", tc.signers), func(t *testing.T) {
			got := k.FeeForMsg(ctx, agreementWith(creator, tc.signers))
			require.Equal(t, math.NewInt(tc.want), got, tc.why)
		})
	}

	params := k.GetParams(ctx)
	base := math.NewInt(50_000)
	for n := 1; n <= 40; n++ {
		fee := k.FeeForMsg(ctx, agreementWith(creator, n))
		extras := int64(0)
		if n > int(params.AgreementFreeSigners) {
			extras = int64(n) - int64(params.AgreementFreeSigners)
		}
		require.Equal(t, base.Add(math.NewInt(5_000).MulRaw(extras)), fee,
			"%d signers: surcharge must be exactly 5,000 uphi x %d", n, extras)
	}

	msg := agreementWith(creator, 9)
	require.Equal(t, k.FeeForMsg(ctx, msg), k.FeeForMsg(ctx, msg))
}

// Boundedness: x/credentials rejects an agreement above its max_agreement_signers param, so the surcharge cannot run away.
func TestFeeForMsg_AgreementFeeIsBoundedByTheCredentialsCap(t *testing.T) {
	ctx, k, _, _ := setupCoin(t)
	creator := sdk.AccAddress([]byte("agreement_creator___"))

	maxSigners := int(credentialstypes.DefaultMaxAgreementSigners)
	worst := k.FeeForMsg(ctx, agreementWith(creator, maxSigners))
	require.Equal(t, math.NewInt(525_000), worst, "0.05 + (100-5) x 0.005 = 0.525 PHI")
}

// Every other message keeps exactly the fee it had before the schedule landed.
func TestFeeForMsg_OtherMessagesUnchanged(t *testing.T) {
	ctx, k, _, _ := setupCoin(t)
	payer := sdk.AccAddress([]byte("payer_______________"))

	transfer := &types.MsgTransfer{From: payer.String(), To: payer.String(), Amount: "100000"}
	require.Equal(t, math.NewInt(5_000), k.FeeForMsg(ctx, transfer), "the transfer fee is untouched")

	untabled := &types.MsgUpdateParams{Authority: payer.String(), Params: types.DefaultParams()}
	require.Equal(t, math.NewInt(5_000), k.FeeForMsg(ctx, untabled), "the default fee is untouched")

	cred := &credentialstypes.MsgAnchorCredential{Issuer: payer.String(), IssuerDid: "did:phi:i", TemplateId: "t"}
	require.Equal(t, math.NewInt(5_000), k.FeeForMsg(ctx, cred), "the cert anchor amount is not part of this slice")
}

// The surcharge is routed by the SAME split as the base fee it rides on: an agreement is not in the split table, so its whole fee - base plus surcharge - goes to the validator fee collector.
func TestComputeFeeSplit_NewFeesRouteThroughTheSliceASplit(t *testing.T) {
	ctx, k, _, _ := setupCoin(t)
	creator := sdk.AccAddress([]byte("agreement_creator___"))
	owner := sdk.AccAddress([]byte("anchor_owner________"))

	agreement := k.ComputeFeeSplit(ctx, []sdk.Msg{agreementWith(creator, 10)})
	require.Equal(t, math.NewInt(75_000), agreement.Total)
	require.Equal(t, agreement.Total, agreement.Validator, "an agreement pays the validator pool in full")
	require.True(t, agreement.Company.IsZero())

	personal := k.ComputeFeeSplit(ctx, []sdk.Msg{
		&credentialstypes.MsgAnchorPersonal{Owner: owner.String(), OwnerDid: "did:phi:x"},
	})
	require.Equal(t, math.NewInt(120_000), personal.Total)
	require.Equal(t, personal.Total, personal.Company, "a personal anchor is 100% company revenue")
	require.True(t, personal.Validator.IsZero())
}

// The new schedule and the surcharge params must survive a genesis round-trip; a chain restart must not silently reprice the network.
func TestGenesis_RoundTripsTheFeeSchedule(t *testing.T) {
	ctx, k, _, _ := setupCoin(t)

	gs := types.DefaultGenesis()
	require.NoError(t, gs.Validate())
	k.InitGenesis(ctx, *gs)
	got := k.ExportGenesis(ctx)

	require.Equal(t, gs.Params.Fees, got.Params.Fees, "the fee table must survive the round-trip")
	require.Equal(t, types.DefaultAgreementExtraSignerFee, got.Params.AgreementExtraSignerFee)
	require.Equal(t, types.DefaultAgreementFreeSigners, got.Params.AgreementFreeSigners)
	require.Equal(t, *gs, *got)

	require.Equal(t, math.NewInt(120_000), got.Params.FeeFor(types.MsgAnchorPersonalTypeURL))
	require.Equal(t, math.NewInt(50_000), got.Params.FeeFor(types.MsgCreateAgreementTypeURL))
}
