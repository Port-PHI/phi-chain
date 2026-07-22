// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"cosmossdk.io/math"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/app"
	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	instkeeper "github.com/Port-PHI/phi-chain/x/institutions/keeper"
	insttypes "github.com/Port-PHI/phi-chain/x/institutions/types"
)

// TestSolvency_PreservedByRandomHandlerSequences is a property test: the GLOBAL solvency invariant (supply_uphi × 100000 == Σ vault_toman × 1e6, and every vault ≥ 0) must survive ANY random interleaving of state-changing handlers.
func TestSolvency_PreservedByRandomHandlerSequences(t *testing.T) {
	for _, seed := range []int64{1, 42, 1337, 2024, 90210} {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			runSolvencyProperty(t, seed, 150)
		})
	}
}

type solvencyWorld struct {
	t       *testing.T
	app     *app.App
	ctx     sdk.Context
	imsg    insttypes.MsgServer
	oper    sdk.AccAddress
	insts   []string
	holders []sdk.AccAddress
	ref     int
}

func runSolvencyProperty(t *testing.T, seed int64, steps int) {
	t.Helper()
	a := newTestApp(t)
	ctx := a.NewUncachedContext(false, cmtproto.Header{Height: 10, Time: time.Unix(1_700_000_000, 0).UTC()})

	oper := sdk.AccAddress([]byte("prop-operator-acct__"))
	require.NoError(t, a.InstitutionsKeeper.SetParams(ctx, insttypes.Params{
		Operator:                       oper.String(),
		PenaltyDestination:             oper.String(),
		PhiToToman:                     insttypes.DefaultPhiToToman,
		ProtocolFeeBps:                 20,
		RedeemFloorPerTx:               insttypes.DefaultRedeemFloorToman,
		RedeemDailyCapPerDidUphi:       "", // no network per-DID cap: the property run should not trip a rate limit
		MaxAttestationStalenessSeconds: insttypes.DefaultMaxAttestationStalenessSeconds,
	}))
	govParams := govv1.DefaultParams()
	govParams.BurnVoteVeto = true
	require.NoError(t, a.GovKeeper.Params.Set(ctx, govParams))

	imsg := instkeeper.NewMsgServerImpl(a.InstitutionsKeeper)
	w := &solvencyWorld{t: t, app: a, ctx: ctx, imsg: imsg, oper: oper}

	for i := 0; i < 2; i++ {
		id := fmt.Sprintf("bank-%d", i)
		_, err := imsg.RegisterInstitution(ctx, &insttypes.MsgRegisterInstitution{
			Operator: oper.String(), Id: id, License: "LIC", Admin: oper.String(),
			VaultAccount: "v", VaultApi: "x", Bond: "0", InstitutionType: insttypes.INSTITUTION_TYPE_FINANCIAL,
		})
		require.NoError(t, err)
		compliance := sdk.AccAddress([]byte(fmt.Sprintf("prop-compliance-%03d ", i)))
		a.InstitutionsKeeper.SetRole(ctx, id, compliance, insttypes.INSTITUTION_ROLE_COMPLIANCE)
		a.InstitutionsKeeper.SetRole(ctx, id, sdk.AccAddress([]byte("second-admin-key____")), insttypes.INSTITUTION_ROLE_ADMIN)
		pinSensitiveThreshold(t, a, ctx, id)
		_, err = imsg.PublishInstitutionAttestation(ctx, &insttypes.MsgPublishInstitutionAttestation{
			Admin: compliance.String(), Institution: id, AttestedReserve: "1000000000000",
		})
		require.NoError(t, err)
		w.insts = append(w.insts, id)
	}
	for i := 0; i < 3; i++ {
		w.holders = append(w.holders, sdk.AccAddress([]byte(fmt.Sprintf("prop-holder-%09d", i))))
	}

	w.requireSolvent() // holds trivially at the empty start
	rng := rand.New(rand.NewSource(seed))
	for step := 0; step < steps; step++ {
		switch rng.Intn(5) {
		case 0:
			w.mint(rng)
		case 1:
			w.redeem(rng)
		case 2:
			w.govBurn(rng)
		case 3:
			w.transfer(rng)
		case 4:
			w.drainRevenue()
		}
		w.requireSolvent() // after EVERY handler, no decoupling is tolerated
	}
}

func (w *solvencyWorld) nextRef() string {
	w.ref++
	return fmt.Sprintf("ref-%d", w.ref)
}

func (w *solvencyWorld) bal(a sdk.AccAddress) math.Int {
	return w.app.BankKeeper.GetBalance(w.ctx, a, cointypes.Denom).Amount
}

func (w *solvencyWorld) vaultToman(id string) math.Int {
	inst, found := w.app.InstitutionsKeeper.GetInstitution(w.ctx, id)
	require.True(w.t, found)
	v, ok := math.NewIntFromString(inst.VaultBalance)
	require.True(w.t, ok)
	return v
}

func (w *solvencyWorld) requireSolvent() {
	msg, broken := instkeeper.SolvencyInvariant(w.app.InstitutionsKeeper)(w.ctx)
	require.False(w.t, broken, "REGISTERED solvency invariant broken: %s", msg)
	msg, broken = instkeeper.NonNegativeVaultInvariant(w.app.InstitutionsKeeper)(w.ctx)
	require.False(w.t, broken, "vault went negative: %s", msg)
}

func (w *solvencyWorld) mint(rng *rand.Rand) {
	inst := w.insts[rng.Intn(len(w.insts))]
	holder := w.holders[rng.Intn(len(w.holders))]
	toman := int64(rng.Intn(10_000) + 1)
	_, err := w.imsg.InstitutionMint(w.ctx, &insttypes.MsgInstitutionMint{
		Admin: w.oper.String(), Institution: inst, Recipient: holder.String(),
		AmountToman: fmt.Sprintf("%d", toman), DepositRef: w.nextRef(),
	})
	require.NoError(w.t, err, "a within-backing mint must succeed")
}

func (w *solvencyWorld) redeem(rng *rand.Rand) {
	holder := w.holders[rng.Intn(len(w.holders))]
	inst := w.insts[rng.Intn(len(w.insts))]
	k := int64(10) // uphi per toman at the fixed rate; redeem uphi is always a multiple of k
	maxTomanFromHolder := w.bal(holder).QuoRaw(k)
	maxTomanFromVault := w.vaultToman(inst)
	limit := math.MinInt(maxTomanFromHolder, maxTomanFromVault)
	if !limit.IsPositive() {
		return
	}
	capToman := limit.Int64()
	if capToman > 5_000 {
		capToman = 5_000
	}
	toman := int64(rng.Intn(int(capToman)) + 1)
	_, err := w.imsg.InstitutionRedeem(w.ctx, &insttypes.MsgInstitutionRedeem{
		Admin: holder.String(), Institution: inst, Holder: holder.String(),
		AmountToman: fmt.Sprintf("%d", toman), RedeemRef: w.nextRef(),
	})
	require.NoError(w.t, err, "a within-vault, within-balance redemption must succeed")
}

func (w *solvencyWorld) govBurn(rng *rand.Rand) {
	holder := w.holders[rng.Intn(len(w.holders))]
	have := w.bal(holder)
	if !have.IsPositive() {
		return
	}
	amt := math.NewInt(rng.Int63n(have.Int64()) + 1)
	propID := uint64(w.ref + 1)
	w.ref++
	require.NoError(w.t, w.app.BankKeeper.SendCoinsFromAccountToModule(w.ctx, holder, govtypes.ModuleName, cointypes.CoinsOf(amt)))
	require.NoError(w.t, w.app.GovKeeper.SetDeposit(w.ctx, govv1.Deposit{
		ProposalId: propID, Depositor: holder.String(), Amount: cointypes.CoinsOf(amt),
	}))
	require.NoError(w.t, w.app.GovKeeper.DeleteAndBurnDeposits(w.ctx, propID))
}

func (w *solvencyWorld) transfer(rng *rand.Rand) {
	from := w.holders[rng.Intn(len(w.holders))]
	to := w.holders[rng.Intn(len(w.holders))]
	have := w.bal(from)
	if from.Equals(to) || !have.IsPositive() {
		return
	}
	amt := math.NewInt(rng.Int63n(have.Int64()) + 1)
	require.NoError(w.t, w.app.BankKeeper.SendCoins(w.ctx, from, to, cointypes.CoinsOf(amt)))
}

func (w *solvencyWorld) drainRevenue() {
	rev := w.app.AccountKeeper.GetModuleAddress(cointypes.RevenueAccountName)
	have := w.app.BankKeeper.GetBalance(w.ctx, rev, cointypes.Denom).Amount
	if !have.IsPositive() {
		return
	}
	require.NoError(w.t, w.app.BankKeeper.SendCoinsFromModuleToAccount(w.ctx, cointypes.RevenueAccountName, w.oper, cointypes.CoinsOf(have)))
}
