// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/app"
	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	instkeeper "github.com/Port-PHI/phi-chain/x/institutions/keeper"
	insttypes "github.com/Port-PHI/phi-chain/x/institutions/types"
)

type revenueChain struct {
	app     *app.App
	ctx     sdk.Context
	imsg    insttypes.MsgServer
	oper    sdk.AccAddress
	holder  sdk.AccAddress
	revenue sdk.AccAddress
}

func setupProtocolFee(t *testing.T, protocolFeeBps uint32) revenueChain {
	t.Helper()
	a := newTestApp(t)
	ctx := a.NewUncachedContext(false, cmtproto.Header{Height: 10, Time: time.Unix(1_700_000_000, 0).UTC()})

	oper := sdk.AccAddress([]byte("inst-operator-acct__"))
	holder := sdk.AccAddress([]byte("coin-holder-account_"))
	require.NoError(t, a.InstitutionsKeeper.SetParams(ctx, insttypes.Params{
		Operator:           oper.String(),
		PenaltyDestination: oper.String(),
		PhiToToman:         insttypes.DefaultPhiToToman,
		RedeemFloorPerTx:   insttypes.DefaultRedeemFloorToman,
		ProtocolFeeBps:     protocolFeeBps,
	}))

	imsg := instkeeper.NewMsgServerImpl(a.InstitutionsKeeper)
	_, err := imsg.RegisterInstitution(ctx, &insttypes.MsgRegisterInstitution{
		Operator: oper.String(), Id: "bank-a", License: "LIC-1", Admin: oper.String(),
		VaultAccount: "v", VaultApi: "x", Bond: "0", InstitutionType: insttypes.INSTITUTION_TYPE_FINANCIAL,
	})
	require.NoError(t, err)
	compliance := sdk.AccAddress([]byte("compliance-officer__"))
	a.InstitutionsKeeper.SetRole(ctx, "bank-a", compliance, insttypes.INSTITUTION_ROLE_COMPLIANCE)
	a.InstitutionsKeeper.SetRole(ctx, "bank-a", sdk.AccAddress([]byte("second-admin-key____")), insttypes.INSTITUTION_ROLE_ADMIN)
	pinSensitiveThreshold(t, a, ctx, "bank-a")
	_, err = imsg.PublishInstitutionAttestation(ctx, &insttypes.MsgPublishInstitutionAttestation{
		Admin: compliance.String(), Institution: "bank-a", AttestedReserve: "100000000",
	})
	require.NoError(t, err)

	return revenueChain{
		app: a, ctx: ctx, imsg: imsg, oper: oper, holder: holder,
		revenue: a.AccountKeeper.GetModuleAddress(cointypes.RevenueAccountName),
	}
}

func (c revenueChain) supply() math.Int {
	return c.app.BankKeeper.GetSupply(c.ctx, cointypes.Denom).Amount
}

func (c revenueChain) balance(a sdk.AccAddress) math.Int {
	return c.app.BankKeeper.GetBalance(c.ctx, a, cointypes.Denom).Amount
}

func (c revenueChain) vault(t *testing.T) math.Int {
	t.Helper()
	inst, found := c.app.InstitutionsKeeper.GetInstitution(c.ctx, "bank-a")
	require.True(t, found)
	v, ok := math.NewIntFromString(inst.VaultBalance)
	require.True(t, ok)
	return v
}

func (c revenueChain) requireSolvent(t *testing.T) {
	t.Helper()
	msg, broken := instkeeper.SolvencyInvariant(c.app.InstitutionsKeeper)(c.ctx)
	require.False(t, broken, "REGISTERED solvency invariant broken: %s", msg)

	msg, broken = instkeeper.NonNegativeVaultInvariant(c.app.InstitutionsKeeper)(c.ctx)
	require.False(t, broken, "vault went negative: %s", msg)

	msg, broken = instkeeper.BackingShortfallInvariant(c.app.InstitutionsKeeper)(c.ctx)
	require.False(t, broken, "institution vault exceeds its attested reserve: %s", msg)
}

func (c revenueChain) mint(t *testing.T, toman, ref string) {
	t.Helper()
	_, err := c.imsg.InstitutionMint(c.ctx, &insttypes.MsgInstitutionMint{
		Admin: c.oper.String(), Institution: "bank-a", Recipient: c.holder.String(),
		AmountToman: toman, DepositRef: ref,
	})
	require.NoError(t, err)
}

func (c revenueChain) redeem(t *testing.T, toman, ref string) *insttypes.MsgInstitutionRedeemResponse {
	t.Helper()
	res, err := c.imsg.InstitutionRedeem(c.ctx, &insttypes.MsgInstitutionRedeem{
		Admin: c.holder.String(), Institution: "bank-a", Holder: c.holder.String(),
		AmountToman: toman, RedeemRef: ref,
	})
	require.NoError(t, err)
	return res
}

// MINT: supply rises by the full N, the vault by the full toman; the mint splits into customer (N-fee) + phi_revenue (fee).
func TestProtocolFee_MintIsSupplyNeutralInTheInvariant(t *testing.T) {
	c := setupProtocolFee(t, 20) // 0.2%
	supplyBefore := c.supply()

	c.mint(t, "100000", "dep-1") // 100,000 toman -> 1,000,000 uphi

	const minted = 1_000_000
	const fee = 2_000 // 0.2% of 1,000,000
	require.Equal(t, supplyBefore.AddRaw(minted).String(), c.supply().String(),
		"supply rises by the FULL minted amount, not by the customer's net")
	require.Equal(t, math.NewInt(minted-fee).String(), c.balance(c.holder).String(),
		"the customer receives the mint minus the protocol fee")
	require.Equal(t, math.NewInt(fee).String(), c.balance(c.revenue).String(),
		"phi_revenue receives the protocol cut")
	require.Equal(t, math.NewInt(100_000).String(), c.vault(t).String(),
		"the vault rises by the FULL toman: it backs the protocol's uphi too")

	require.Equal(t, c.balance(c.holder).Add(c.balance(c.revenue)).String(), math.NewInt(minted).String(),
		"customer + phi_revenue == the whole mint; nothing was created or lost by the split")
	c.requireSolvent(t)
}

// REDEEM, young coin (<7 days -> 1% penalty): supply falls by the BURNED amount, not the amount surrendered.
func TestProtocolFee_RedeemYoungBurnsOnlyTheBurnedAmount(t *testing.T) {
	c := setupProtocolFee(t, 20)
	c.mint(t, "100000", "dep-1") // customer holds 998,000 uphi (young, from now)
	c.requireSolvent(t)

	supplyBefore, vaultBefore, revenueBefore := c.supply(), c.vault(t), c.balance(c.revenue)

	res := c.redeem(t, "10000", "red-1")

	const surrendered = 100_000
	const burned = 98_800
	const carved = 1_200

	require.Equal(t, math.NewInt(burned).String(), res.BurnedUphi, "the response reports what was BURNED")
	require.Equal(t, supplyBefore.SubRaw(burned).String(), c.supply().String(),
		"supply falls by exactly the BURNED amount (98,800), NOT by the 100,000 surrendered")
	require.Equal(t, revenueBefore.AddRaw(carved).String(), c.balance(c.revenue).String(),
		"the carve-out (fee 200 + penalty 1,000) is still in circulation, in phi_revenue")
	require.Equal(t, vaultBefore.SubRaw(9_880).String(), c.vault(t).String(),
		"the vault falls by the BURNED amount's toman (9,880), NOT by the 10,000 surrendered")

	supplyDrop := supplyBefore.Sub(c.supply())
	revenueGain := c.balance(c.revenue).Sub(revenueBefore)
	require.Equal(t, math.NewInt(surrendered).String(), supplyDrop.Add(revenueGain).String(),
		"burned + carved == surrendered")

	c.requireSolvent(t)
}

// REDEEM, old coin (>=7 days -> 0.2% penalty): same accounting, different tier.
func TestProtocolFee_RedeemOldCoinLowerPenaltyStillSolvent(t *testing.T) {
	c := setupProtocolFee(t, 20)
	c.mint(t, "100000", "dep-1")

	c.ctx = c.ctx.WithBlockTime(c.ctx.BlockTime().Add(8 * 24 * time.Hour))

	supplyBefore, vaultBefore, revenueBefore := c.supply(), c.vault(t), c.balance(c.revenue)

	res := c.redeem(t, "10000", "red-1")

	require.Equal(t, math.NewInt(99_600).String(), res.BurnedUphi)
	require.Equal(t, supplyBefore.SubRaw(99_600).String(), c.supply().String(),
		"supply falls by exactly the burned amount")
	require.Equal(t, revenueBefore.AddRaw(400).String(), c.balance(c.revenue).String(),
		"old coin pays the 0.2% penalty, not the 1% young rate")
	require.Equal(t, vaultBefore.SubRaw(9_960).String(), c.vault(t).String(),
		"the vault falls by the burned amount's toman")
	c.requireSolvent(t)
}

// DIVISIBILITY: rounding dust joins the carve-out, the burn stays a multiple of k, and supply reconciles.
func TestProtocolFee_RedeemDustIsAccountedAndSupplyConserved(t *testing.T) {
	c := setupProtocolFee(t, 20)
	c.mint(t, "100000", "dep-1")

	supplyBefore, vaultBefore, revenueBefore := c.supply(), c.vault(t), c.balance(c.revenue)

	res := c.redeem(t, "101", "red-1")

	require.Equal(t, math.NewInt(990).String(), res.BurnedUphi, "the burn is a multiple of k=10")
	require.Equal(t, supplyBefore.SubRaw(990).String(), c.supply().String())
	require.Equal(t, revenueBefore.AddRaw(20).String(), c.balance(c.revenue).String(),
		"the 8 uphi of dust joined the carve-out; it did not vanish")
	require.Equal(t, vaultBefore.SubRaw(99).String(), c.vault(t).String(),
		"990 uphi = 99 toman exactly — the vault decrement is never a truncated fraction")

	supplyDrop := supplyBefore.Sub(c.supply())
	revenueGain := c.balance(c.revenue).Sub(revenueBefore)
	require.Equal(t, math.NewInt(1_010).String(), supplyDrop.Add(revenueGain).String())

	c.requireSolvent(t)
}

// protocol_fee_bps = 0 disables the protocol cut but the early-redeem penalty still routes to phi_revenue.
func TestProtocolFee_ZeroDisablesTheFeeButNotThePenalty(t *testing.T) {
	c := setupProtocolFee(t, 0)
	c.mint(t, "100000", "dep-1")

	require.Equal(t, math.NewInt(1_000_000).String(), c.balance(c.holder).String(),
		"protocol_fee_bps=0: the customer receives the full mint")
	require.True(t, c.balance(c.revenue).IsZero(), "no protocol fee was carved on the mint")
	c.requireSolvent(t)

	supplyBefore, vaultBefore := c.supply(), c.vault(t)

	res := c.redeem(t, "10000", "red-1")

	require.Equal(t, math.NewInt(99_000).String(), res.BurnedUphi)
	require.Equal(t, math.NewInt(1_000).String(), c.balance(c.revenue).String(),
		"the early-redeem penalty still routes to phi_revenue — it is network revenue, not an "+
			"off-chain rial cut retained by the institution")
	require.Equal(t, supplyBefore.SubRaw(99_000).String(), c.supply().String(),
		"supply falls by exactly the burned amount")
	require.Equal(t, vaultBefore.SubRaw(9_900).String(), c.vault(t).String())
	c.requireSolvent(t)
}

// The invariant must hold across a sequence of mints and redemptions at different coin ages.
func TestProtocolFee_SolvencyHoldsAcrossAMintRedeemSequence(t *testing.T) {
	c := setupProtocolFee(t, 20)

	c.mint(t, "50000", "dep-1")
	c.requireSolvent(t)
	c.mint(t, "37", "dep-2")
	c.requireSolvent(t)

	c.redeem(t, "101", "red-1")
	c.requireSolvent(t)
	c.redeem(t, "7", "red-2")
	c.requireSolvent(t)

	c.ctx = c.ctx.WithBlockTime(c.ctx.BlockTime().Add(9 * 24 * time.Hour))
	c.mint(t, "999", "dep-3")
	c.requireSolvent(t)
	c.redeem(t, "1234", "red-3")
	c.requireSolvent(t)
	c.redeem(t, "3", "red-4")
	c.requireSolvent(t)

	require.True(t, c.balance(c.revenue).IsPositive(), "the protocol accrued revenue over the sequence")
}

// FIFO across the tier boundary: the penalty is the per-lot sum (old 0.2% + young 1%), supply falls by the burned amount.
func TestFIFO_RedeemAcrossTheTierBoundaryIsSolvent(t *testing.T) {
	c := setupProtocolFee(t, 0) // protocol fee off, so the coin-age penalty is the only carve-out

	c.mint(t, "100000", "dep-1") // Lot 1
	c.ctx = c.ctx.WithBlockTime(c.ctx.BlockTime().Add(8 * 24 * time.Hour))
	c.mint(t, "100000", "dep-2") // Lot 2, fresh
	c.requireSolvent(t)

	supplyBefore, vaultBefore, revenueBefore := c.supply(), c.vault(t), c.balance(c.revenue)

	res := c.redeem(t, "150000", "red-1")

	require.Equal(t, math.NewInt(1_493_000).String(), res.BurnedUphi)
	require.Equal(t, revenueBefore.AddRaw(7_000).String(), c.balance(c.revenue).String(),
		"the penalty is the PER-LOT sum (2,000 + 5,000), not the 9,000 a blended rate would charge")
	require.Equal(t, supplyBefore.SubRaw(1_493_000).String(), c.supply().String(),
		"supply falls by exactly the BURNED amount — Slice B's accounting is unchanged by FIFO")
	require.Equal(t, vaultBefore.SubRaw(149_300).String(), c.vault(t).String(),
		"the vault falls by the burned amount's toman")

	supplyDrop := supplyBefore.Sub(c.supply())
	revenueGain := c.balance(c.revenue).Sub(revenueBefore)
	require.Equal(t, math.NewInt(1_500_000).String(), supplyDrop.Add(revenueGain).String())

	c.requireSolvent(t)
}

// OLD-heavy and YOUNG-heavy redemptions both stay solvent with both carve-outs live.
func TestFIFO_OldHeavyAndYoungHeavyBothStaySolvent(t *testing.T) {
	old := setupProtocolFee(t, 20)
	old.mint(t, "100000", "dep-1")
	old.ctx = old.ctx.WithBlockTime(old.ctx.BlockTime().Add(10 * 24 * time.Hour))

	supplyBefore, vaultBefore, revenueBefore := old.supply(), old.vault(t), old.balance(old.revenue)
	res := old.redeem(t, "10000", "red-1")
	require.Equal(t, math.NewInt(99_600).String(), res.BurnedUphi)
	require.Equal(t, supplyBefore.SubRaw(99_600).String(), old.supply().String(),
		"supply falls by exactly the burned amount")
	require.Equal(t, revenueBefore.AddRaw(400).String(), old.balance(old.revenue).String())
	require.Equal(t, vaultBefore.SubRaw(9_960).String(), old.vault(t).String())
	old.requireSolvent(t)

	young := setupProtocolFee(t, 20)
	young.mint(t, "100000", "dep-1")

	supplyBefore, vaultBefore, revenueBefore = young.supply(), young.vault(t), young.balance(young.revenue)
	res = young.redeem(t, "10000", "red-1")
	require.Equal(t, math.NewInt(98_800).String(), res.BurnedUphi)
	require.Equal(t, supplyBefore.SubRaw(98_800).String(), young.supply().String(),
		"supply falls by exactly the burned amount")
	require.Equal(t, revenueBefore.AddRaw(1_200).String(), young.balance(young.revenue).String())
	require.Equal(t, vaultBefore.SubRaw(9_880).String(), young.vault(t).String())
	young.requireSolvent(t)
}
