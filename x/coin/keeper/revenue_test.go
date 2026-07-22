// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/coin/types"
	credentialstypes "github.com/Port-PHI/phi-chain/x/credentials/types"
)

func legOf(split types.FeeSplit, msgTypeURL, stream string) math.Int {
	for _, l := range split.Legs {
		if l.MsgTypeURL == msgTypeURL && l.Stream == stream {
			return l.Amount
		}
	}
	return math.ZeroInt()
}

// A transfer fee splits 90/10: the validator leg goes to the fee collector, the company leg to phi_revenue, and the two sum to the fee EXACTLY (no dust minted, none stranded).
func TestComputeFeeSplit_TransferIs90_10(t *testing.T) {
	ctx, k, _, _ := setupCoin(t)
	payer := sdk.AccAddress([]byte("payer_______________"))
	transfer := &types.MsgTransfer{From: payer.String(), To: payer.String(), Amount: "100000"}

	split := k.ComputeFeeSplit(ctx, []sdk.Msg{transfer})

	require.Equal(t, math.NewInt(5_000), split.Total, "the transfer fee is unchanged by the split")
	require.Equal(t, math.NewInt(4_500), split.Validator, "90% of the fee goes to the validators")
	require.Equal(t, math.NewInt(500), split.Company, "10% of the fee goes to the company")
	require.Equal(t, split.Total, split.Validator.Add(split.Company), "the parts must sum to the fee exactly")

	require.Equal(t, math.NewInt(4_500), legOf(split, types.MsgTransferTypeURL, types.StreamValidator))
	require.Equal(t, math.NewInt(500), legOf(split, types.MsgTransferTypeURL, types.StreamCompany),
		"the company stream must be distinguishable in the events")
}

// A personal anchor is a pure protocol service: 100% of its fee is company revenue and the validators take nothing.
func TestComputeFeeSplit_AnchorPersonalIsAllCompany(t *testing.T) {
	ctx, k, _, _ := setupCoin(t)
	owner := sdk.AccAddress([]byte("anchor_owner________"))
	anchor := &credentialstypes.MsgAnchorPersonal{Owner: owner.String(), OwnerDid: "did:phi:x"}

	split := k.ComputeFeeSplit(ctx, []sdk.Msg{anchor})

	require.Equal(t, types.MsgAnchorPersonalTypeURL, sdk.MsgTypeURL(anchor), "the split table must key the real type URL")
	require.Equal(t, math.NewInt(120_000), split.Total, "a personal anchor costs 0.12 PHI")
	require.True(t, split.Validator.IsZero(), "a personal anchor pays the validators nothing")
	require.Equal(t, split.Total, split.Company, "the whole fee is company revenue")
	require.True(t, legOf(split, types.MsgAnchorPersonalTypeURL, types.StreamValidator).IsZero(),
		"a zero leg must not be emitted at all")
}

// A message type absent from the split table keeps the pre-split behaviour: its whole fee goes to the validator fee collector, exactly as before this engine existed.
func TestComputeFeeSplit_UnknownMsgKeepsFlatValidatorFee(t *testing.T) {
	ctx, k, _, _ := setupCoin(t)
	authority := sdk.AccAddress([]byte("gov_authority_______"))
	notInTable := &types.MsgUpdateParams{Authority: authority.String(), Params: types.DefaultParams()}

	_, found := k.GetParams(ctx).SplitFor(sdk.MsgTypeURL(notInTable))
	require.False(t, found, "the fixture must genuinely be absent from the table")

	split := k.ComputeFeeSplit(ctx, []sdk.Msg{notInTable})
	require.Equal(t, math.NewInt(5_000), split.Total, "the default fee still applies")
	require.Equal(t, split.Total, split.Validator, "an unsplit message pays the fee collector in full")
	require.True(t, split.Company.IsZero(), "no company share for a message outside the table")
}

// Rounding rule: the company share is taken by bps and the validator leg is the REMAINDER, so an indivisible fee still sums exactly.
func TestComputeFeeSplit_IndivisibleFeeSumsExactly(t *testing.T) {
	ctx, k, _, _ := setupCoin(t)
	params := types.DefaultParams()
	params.Fees = []types.FeeEntry{{MsgTypeUrl: types.MsgTransferTypeURL, Fee: "5001"}}
	require.NoError(t, k.SetParams(ctx, params))

	payer := sdk.AccAddress([]byte("payer_______________"))
	transfer := &types.MsgTransfer{From: payer.String(), To: payer.String(), Amount: "100000"}

	split := k.ComputeFeeSplit(ctx, []sdk.Msg{transfer})
	require.Equal(t, math.NewInt(5_001), split.Total)
	require.Equal(t, math.NewInt(500), split.Company, "the company share is rounded DOWN, never up")
	require.Equal(t, math.NewInt(4_501), split.Validator, "the validator leg absorbs the remainder")
	require.Equal(t, split.Total, split.Validator.Add(split.Company))
}

// A multi-message tx accumulates each message's split independently, and the totals still reconcile.
func TestComputeFeeSplit_MixedTxAccumulatesPerMessage(t *testing.T) {
	ctx, k, _, _ := setupCoin(t)
	payer := sdk.AccAddress([]byte("payer_______________"))
	msgs := []sdk.Msg{
		&types.MsgTransfer{From: payer.String(), To: payer.String(), Amount: "100000"},
		&credentialstypes.MsgAnchorPersonal{Owner: payer.String(), OwnerDid: "did:phi:x"},
	}

	split := k.ComputeFeeSplit(ctx, msgs)
	require.Equal(t, math.NewInt(125_000), split.Total, "5,000 (transfer) + 120,000 (personal anchor)")
	require.Equal(t, math.NewInt(4_500), split.Validator, "only the transfer pays the validators")
	require.Equal(t, math.NewInt(120_500), split.Company, "500 from the transfer + 120,000 from the anchor")
	require.Equal(t, split.Total, split.Validator.Add(split.Company))
	require.Equal(t, split.Total, k.ComputeRequiredFee(ctx, msgs), "the split must not change what the payer owes")
}

// The only exit from phi_revenue is governance-gated, bounded by the balance, and needs a destination.
func TestWithdrawRevenue_AuthorityGatedAndBounded(t *testing.T) {
	ctx, k, msg, bank := setupCoin(t)
	payout := sdk.AccAddress([]byte("company_payout______"))
	stranger := sdk.AccAddress([]byte("not_the_authority___"))

	bank.bal[types.RevenueAccountName] = math.NewInt(10_000)

	_, err := msg.WithdrawRevenue(ctx, &types.MsgWithdrawRevenue{Authority: k.GetAuthority(), Amount: "1000"})
	require.ErrorIs(t, err, types.ErrNoPayoutAddress)

	params := types.DefaultParams()
	params.CompanyPayoutAddress = payout.String()
	require.NoError(t, k.SetParams(ctx, params))

	_, err = msg.WithdrawRevenue(ctx, &types.MsgWithdrawRevenue{Authority: stranger.String(), Amount: "1000"})
	require.ErrorIs(t, err, govtypes.ErrInvalidSigner)
	require.Equal(t, math.NewInt(10_000), bank.get(types.RevenueAccountName), "a rejected withdrawal moves nothing")

	_, err = msg.WithdrawRevenue(ctx, &types.MsgWithdrawRevenue{Authority: k.GetAuthority(), Amount: "10001"})
	require.ErrorIs(t, err, types.ErrInsufficientFunds)
	require.Equal(t, math.NewInt(10_000), bank.get(types.RevenueAccountName))

	_, err = msg.WithdrawRevenue(ctx, &types.MsgWithdrawRevenue{Authority: k.GetAuthority(), Amount: "6000"})
	require.NoError(t, err)
	require.Equal(t, math.NewInt(6_000), bank.get(payout.String()), "the payout address receives the withdrawal")
	require.Equal(t, math.NewInt(4_000), bank.get(types.RevenueAccountName), "the remainder stays in phi_revenue")
}

// Genesis must carry the split table and the payout address through a full round-trip: a param that survives export but not import (or vice versa) would silently reset revenue routing on a chain restart or a state export.
func TestGenesis_RoundTripsSplitsAndPayoutAddress(t *testing.T) {
	ctx, k, _, _ := setupCoin(t)
	payout := sdk.AccAddress([]byte("company_payout______"))

	gs := types.DefaultGenesis()
	gs.Params.CompanyPayoutAddress = payout.String()
	require.NoError(t, gs.Validate(), "the default genesis + a payout address must validate")

	k.InitGenesis(ctx, *gs)
	got := k.ExportGenesis(ctx)

	require.Equal(t, gs.Params.Splits, got.Params.Splits, "the split table must survive the round-trip")
	require.Equal(t, payout.String(), got.Params.CompanyPayoutAddress)
	require.Equal(t, *gs, *got)

	bad := types.DefaultGenesis()
	bad.Params.Splits = []types.SplitEntry{{MsgTypeUrl: types.MsgTransferTypeURL, ValidatorBps: 9_000, CompanyBps: 1_001}}
	require.Error(t, bad.Validate(), "a split summing over 10,000 bps must fail genesis validation")
}
