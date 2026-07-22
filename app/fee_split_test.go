// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/app"
	phiante "github.com/Port-PHI/phi-chain/app/ante"
	coinkeeper "github.com/Port-PHI/phi-chain/x/coin/keeper"
	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	credentialstypes "github.com/Port-PHI/phi-chain/x/credentials/types"
	instkeeper "github.com/Port-PHI/phi-chain/x/institutions/keeper"
	insttypes "github.com/Port-PHI/phi-chain/x/institutions/types"
)

type feeSplitTx struct {
	sdk.Tx
	msgs  []sdk.Msg
	payer []byte
}

func (t feeSplitTx) GetMsgs() []sdk.Msg { return t.msgs }
func (t feeSplitTx) GetGas() uint64     { return 200_000 }
func (t feeSplitTx) GetFee() sdk.Coins  { return nil }
func (t feeSplitTx) FeePayer() []byte   { return t.payer }
func (t feeSplitTx) FeeGranter() []byte { return nil }
func (feeSplitTx) ValidateBasic() error { return nil }

type revenueFixture struct {
	app          *app.App
	ctx          sdk.Context
	ante         phiante.FixedFeeDecorator
	payer        sdk.AccAddress
	feeCollector sdk.AccAddress
	revenue      sdk.AccAddress
}

func setupRevenue(t *testing.T) revenueFixture {
	t.Helper()
	a := newTestApp(t)
	ctx := a.NewUncachedContext(false, cmtproto.Header{Height: 10, Time: time.Unix(1_700_000_000, 0).UTC()})

	operator := sdk.AccAddress([]byte("inst-operator-acct__"))
	payer := sdk.AccAddress([]byte("fee-payer-account___"))
	require.NoError(t, a.InstitutionsKeeper.SetParams(ctx, insttypes.Params{
		Operator:           operator.String(),
		PenaltyDestination: operator.String(),
		PhiToToman:         insttypes.DefaultPhiToToman,
		RedeemFloorPerTx:   insttypes.DefaultRedeemFloorToman,
	}))

	imsg := instkeeper.NewMsgServerImpl(a.InstitutionsKeeper)
	_, err := imsg.RegisterInstitution(ctx, &insttypes.MsgRegisterInstitution{
		Operator: operator.String(), Id: "bank-a", License: "LIC-1", Admin: operator.String(),
		VaultAccount: "v", VaultApi: "x", Bond: "0", InstitutionType: insttypes.INSTITUTION_TYPE_FINANCIAL,
	})
	require.NoError(t, err)
	compliance := sdk.AccAddress([]byte("compliance-officer__"))
	a.InstitutionsKeeper.SetRole(ctx, "bank-a", compliance, insttypes.INSTITUTION_ROLE_COMPLIANCE)
	a.InstitutionsKeeper.SetRole(ctx, "bank-a", sdk.AccAddress([]byte("second-admin-key____")), insttypes.INSTITUTION_ROLE_ADMIN)
	pinSensitiveThreshold(t, a, ctx, "bank-a")
	_, err = imsg.PublishInstitutionAttestation(ctx, &insttypes.MsgPublishInstitutionAttestation{
		Admin: compliance.String(), Institution: "bank-a", AttestedReserve: "1000000000",
	})
	require.NoError(t, err)
	_, err = imsg.InstitutionMint(ctx, &insttypes.MsgInstitutionMint{
		Admin: operator.String(), Institution: "bank-a", Recipient: payer.String(),
		AmountToman: "100000", DepositRef: "fund-payer", // 1,000,000 uphi
	})
	require.NoError(t, err)

	return revenueFixture{
		app:          a,
		ctx:          ctx,
		ante:         phiante.NewFixedFeeDecorator(a.AccountKeeper, a.BankKeeper, a.FeeGrantKeeper, a.CoinKeeper),
		payer:        payer,
		feeCollector: a.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName),
		revenue:      a.AccountKeeper.GetModuleAddress(cointypes.RevenueAccountName),
	}
}

func (f revenueFixture) balance(addr sdk.AccAddress) math.Int {
	return f.app.BankKeeper.GetBalance(f.ctx, addr, cointypes.Denom).Amount
}

func (f revenueFixture) supply() math.Int {
	return f.app.BankKeeper.GetSupply(f.ctx, cointypes.Denom).Amount
}

func (f revenueFixture) runAnte(t *testing.T, msgs ...sdk.Msg) (payer, feeCollector, revenue math.Int) {
	t.Helper()
	before := struct{ payer, fc, rev math.Int }{f.balance(f.payer), f.balance(f.feeCollector), f.balance(f.revenue)}
	noop := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }
	_, err := f.ante.AnteHandle(f.ctx, feeSplitTx{msgs: msgs, payer: f.payer}, false, noop)
	require.NoError(t, err)
	return before.payer.Sub(f.balance(f.payer)),
		f.balance(f.feeCollector).Sub(before.fc),
		f.balance(f.revenue).Sub(before.rev)
}

// A transfer fee splits 90/10 across the two real module accounts, the parts sum to the exact fee the payer parted with, and — the load-bearing assertion — total supply is untouched: the split is a transfer of already-backed coins, not an issuance.
func TestFeeSplit_TransferRoutes90ToValidators10ToCompany(t *testing.T) {
	f := setupRevenue(t)
	supplyBefore := f.supply()

	transfer := &cointypes.MsgTransfer{From: f.payer.String(), To: f.payer.String(), Amount: "100000"}
	paid, toValidators, toCompany := f.runAnte(t, transfer)

	require.Equal(t, math.NewInt(5_000), paid, "the payer parts with the whole fixed fee")
	require.Equal(t, math.NewInt(4_500), toValidators, "90% lands in the standard fee collector (→ x/distribution)")
	require.Equal(t, math.NewInt(500), toCompany, "10% lands in phi_revenue")
	require.Equal(t, paid, toValidators.Add(toCompany), "the parts must sum to the fee EXACTLY")
	require.Equal(t, supplyBefore, f.supply(), "a fee split must not mint or burn a single uphi")
}

// A personal anchor is 100% company revenue: the validators get nothing and phi_revenue takes the whole fee.
func TestFeeSplit_AnchorPersonalRoutesEntirelyToCompany(t *testing.T) {
	f := setupRevenue(t)
	supplyBefore := f.supply()

	anchor := &credentialstypes.MsgAnchorPersonal{Owner: f.payer.String(), OwnerDid: "did:phi:payer"}
	paid, toValidators, toCompany := f.runAnte(t, anchor)

	require.Equal(t, math.NewInt(120_000), paid, "a personal anchor costs 0.12 PHI")
	require.True(t, toValidators.IsZero(), "a personal anchor pays the validators nothing")
	require.Equal(t, paid, toCompany, "the whole fee is company revenue")
	require.Equal(t, supplyBefore, f.supply(), "no mint, no burn")
}

// A message type outside the split table keeps the pre-split behaviour exactly: the whole fee goes to the fee collector and phi_revenue is never touched.
func TestFeeSplit_UntabledMsgKeepsFlatFeeCollectorBehaviour(t *testing.T) {
	f := setupRevenue(t)
	supplyBefore := f.supply()

	untabled := &cointypes.MsgUpdateParams{Authority: f.payer.String(), Params: cointypes.DefaultParams()}
	paid, toValidators, toCompany := f.runAnte(t, untabled)

	require.Equal(t, math.NewInt(5_000), paid, "the default fee still applies")
	require.Equal(t, paid, toValidators, "the whole fee goes to the fee collector, as before the split engine")
	require.True(t, toCompany.IsZero(), "phi_revenue must not receive anything for an untabled message")
	require.Equal(t, supplyBefore, f.supply())
}

// A payer who cannot cover the whole fee has NO leg of the split committed: baseapp runs the ante on a cached store and discards it on error, so a partial split can never reach committed state.
func TestFeeSplit_PartialFeeIsNeverCommitted(t *testing.T) {
	f := setupRevenue(t)
	broke := sdk.AccAddress([]byte("broke-fee-payer_____"))
	require.NoError(t, f.app.BankKeeper.SendCoins(f.ctx, f.payer, broke, cointypes.CoinsOf(math.NewInt(4_000))))

	supplyBefore, revenueBefore, fcBefore := f.supply(), f.balance(f.revenue), f.balance(f.feeCollector)

	cached, _ := f.ctx.CacheContext() // exactly what baseapp does around the ante
	noop := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }
	transfer := &cointypes.MsgTransfer{From: broke.String(), To: f.payer.String(), Amount: "100000"}
	_, err := f.ante.AnteHandle(cached, feeSplitTx{msgs: []sdk.Msg{transfer}, payer: broke}, false, noop)
	require.ErrorIs(t, err, sdkerrors.ErrInsufficientFunds)

	require.Equal(t, math.NewInt(4_000), f.balance(broke), "the payer keeps every uphi")
	require.Equal(t, revenueBefore, f.balance(f.revenue), "no company leg was committed")
	require.Equal(t, fcBefore, f.balance(f.feeCollector), "no validator leg was committed")
	require.Equal(t, supplyBefore, f.supply())
}

// The revenue account must be structurally incapable of changing supply: keyless (a module address) and registered with NO permissions, so it can neither mint nor burn.
func TestRevenueAccount_IsKeylessPermissionlessAndBlocked(t *testing.T) {
	f := setupRevenue(t)

	acc := f.app.AccountKeeper.GetModuleAccount(f.ctx, cointypes.RevenueAccountName)
	require.NotNil(t, acc, "phi_revenue must be a registered module account")
	require.Empty(t, acc.GetPermissions(), "phi_revenue must hold NO permissions (no Minter, no Burner)")
	require.Nil(t, acc.GetPubKey(), "a module account is keyless: no key exists to steal or to sign with")

	require.Empty(t, app.GetMaccPerms()[cointypes.RevenueAccountName], "the permission list is where mint/burn is denied")
	require.True(t, f.app.BlockedAddresses()[f.revenue.String()],
		"phi_revenue must be a blocked address: value enters it only through the ante's fee routing")
}

// The governance-gated withdrawal is the only exit from phi_revenue, and it is a plain bank send: supply is unchanged, a non-authority signer is refused, and the account cannot be overdrawn.
func TestWithdrawRevenue_GovOnlyAndSupplyNeutral(t *testing.T) {
	f := setupRevenue(t)

	transfer := &cointypes.MsgTransfer{From: f.payer.String(), To: f.payer.String(), Amount: "100000"}
	_, _, accrued := f.runAnte(t, transfer)
	require.Equal(t, math.NewInt(500), accrued)

	payout := sdk.AccAddress([]byte("company-payout-acct_"))
	params := f.app.CoinKeeper.GetParams(f.ctx)
	params.CompanyPayoutAddress = payout.String()
	require.NoError(t, f.app.CoinKeeper.SetParams(f.ctx, params))

	msgSrv := coinkeeper.NewMsgServerImpl(f.app.CoinKeeper)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	supplyBefore := f.supply()

	_, err := msgSrv.WithdrawRevenue(f.ctx, &cointypes.MsgWithdrawRevenue{Authority: f.payer.String(), Amount: "500"})
	require.ErrorIs(t, err, govtypes.ErrInvalidSigner)
	require.Equal(t, math.NewInt(500), f.balance(f.revenue), "a rejected withdrawal moves nothing")

	_, err = msgSrv.WithdrawRevenue(f.ctx, &cointypes.MsgWithdrawRevenue{Authority: authority, Amount: "501"})
	require.ErrorIs(t, err, cointypes.ErrInsufficientFunds)
	require.Equal(t, math.NewInt(500), f.balance(f.revenue))

	_, err = msgSrv.WithdrawRevenue(f.ctx, &cointypes.MsgWithdrawRevenue{Authority: authority, Amount: "500"})
	require.NoError(t, err)
	require.Equal(t, math.NewInt(500), f.balance(payout), "the payout address receives the revenue")
	require.True(t, f.balance(f.revenue).IsZero(), "phi_revenue is emptied")
	require.Equal(t, supplyBefore, f.supply(), "a withdrawal is a bank SEND: it never mints")
}

// The agreement fee is content-dependent: the real ante prices a 10-signer agreement at 0.05 + 5 x 0.005 = 0.075 PHI, charges the payer exactly that, routes it wholly to the validator pool (agreements are not in the split table), and mints nothing.
func TestFeeSchedule_AgreementSurchargeIsChargedByTheAnte(t *testing.T) {
	f := setupRevenue(t)
	supplyBefore := f.supply()

	dids := make([]string, 10)
	for i := range dids {
		dids[i] = fmt.Sprintf("did:phi:signer-%d", i)
	}
	agreement := &credentialstypes.MsgCreateAgreement{
		Creator: f.payer.String(), Hash: []byte("agreement-hash"), RequiredSigners: dids,
	}
	paid, toValidators, toCompany := f.runAnte(t, agreement)

	require.Equal(t, math.NewInt(75_000), paid, "0.05 PHI base + 5 signers beyond the 5th x 0.005 PHI")
	require.Equal(t, paid, toValidators, "the agreement fee (base + surcharge) goes to the validator pool")
	require.True(t, toCompany.IsZero(), "an agreement carries no company share")
	require.Equal(t, supplyBefore, f.supply(), "a content-dependent fee is still a transfer: no mint, no burn")
}

// A 5-signer agreement pays the base fee only - the surcharge starts at the 6th signer.
func TestFeeSchedule_FiveSignerAgreementPaysBaseFeeOnly(t *testing.T) {
	f := setupRevenue(t)
	supplyBefore := f.supply()

	agreement := &credentialstypes.MsgCreateAgreement{
		Creator: f.payer.String(), Hash: []byte("agreement-hash"),
		RequiredSigners: []string{"did:phi:a", "did:phi:b", "did:phi:c", "did:phi:d", "did:phi:e"},
	}
	paid, toValidators, _ := f.runAnte(t, agreement)

	require.Equal(t, math.NewInt(50_000), paid, "the first five signers carry no surcharge")
	require.Equal(t, paid, toValidators)
	require.Equal(t, supplyBefore, f.supply())
}

// A CREDENTIAL anchor pays the ordinary action fee to the validator fee collector and routes NOTHING to phi_revenue.
func TestFeeSplit_CertAnchorPaysTheOrdinaryFeeNotTheCompany(t *testing.T) {
	f := setupRevenue(t)
	supplyBefore := f.supply()

	cert := &credentialstypes.MsgAnchorCredential{
		Issuer: f.payer.String(), IssuerDid: "did:phi:issuer", SubjectDid: "did:phi:subject",
		TemplateId: "edu.degree.v1", TemplateVersion: 1,
	}
	paid, toValidators, toCompany := f.runAnte(t, cert)

	require.Equal(t, math.NewInt(5_000), paid, "a cert anchor pays the ordinary action fee (default_fee)")
	require.Equal(t, paid, toValidators, "the whole cert-anchor fee goes to the validator fee collector")
	require.True(t, toCompany.IsZero(), "a cert anchor must route NOTHING to phi_revenue")
	require.Equal(t, supplyBefore, f.supply(), "no mint, no burn")
}
