// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/coin/keeper"
	"github.com/Port-PHI/phi-chain/x/coin/types"
)

type fakeBank struct{ bal map[string]math.Int }

func newFakeBank() *fakeBank { return &fakeBank{bal: map[string]math.Int{}} }
func (b *fakeBank) get(k string) math.Int {
	if v, ok := b.bal[k]; ok {
		return v
	}
	return math.ZeroInt()
}
func (b *fakeBank) SendCoins(_ context.Context, from, to sdk.AccAddress, amt sdk.Coins) error {
	a := amt.AmountOf(types.Denom)
	b.bal[from.String()] = b.get(from.String()).Sub(a)
	b.bal[to.String()] = b.get(to.String()).Add(a)
	return nil
}
func (b *fakeBank) SendCoinsFromAccountToModule(_ context.Context, from sdk.AccAddress, module string, amt sdk.Coins) error {
	a := amt.AmountOf(types.Denom)
	b.bal[from.String()] = b.get(from.String()).Sub(a)
	b.bal[module] = b.get(module).Add(a)
	return nil
}

// SendCoinsFromModuleToAccount mirrors the real bank on the one property the revenue withdrawal depends on: a module account cannot be overdrawn.
func (b *fakeBank) SendCoinsFromModuleToAccount(_ context.Context, module string, to sdk.AccAddress, amt sdk.Coins) error {
	a := amt.AmountOf(types.Denom)
	if b.get(module).LT(a) {
		return fmt.Errorf("spendable balance %s is smaller than %s: insufficient funds", b.get(module), a)
	}
	b.bal[module] = b.get(module).Sub(a)
	b.bal[to.String()] = b.get(to.String()).Add(a)
	return nil
}
func (b *fakeBank) GetBalance(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewCoin(denom, b.get(addr.String()))
}
func (b *fakeBank) GetSupply(_ context.Context, denom string) sdk.Coin {
	return sdk.NewCoin(denom, math.ZeroInt())
}

type fakeIdentity struct{ dids map[string]string }

func newFakeIdentity() *fakeIdentity { return &fakeIdentity{dids: map[string]string{}} }

func (f *fakeIdentity) identify(addr, did string) { f.dids[addr] = did }

func (f *fakeIdentity) SubjectDID(_ sdk.Context, controller string) (string, bool) {
	did, ok := f.dids[controller]
	return did, ok
}

func setupCoin(t *testing.T) (sdk.Context, keeper.Keeper, types.MsgServer, *fakeBank) {
	ctx, k, msg, bank, _ := setupCoinIdent(t)
	return ctx, k, msg, bank
}

func setupCoinIdent(t *testing.T) (sdk.Context, keeper.Keeper, types.MsgServer, *fakeBank, *fakeIdentity) {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_coin"))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	authority := sdk.AccAddress([]byte("gov_authority_______")).String()
	bank := newFakeBank()
	ident := newFakeIdentity()
	k := keeper.NewKeeper(cdc, key, authority, bank, ident)
	require.NoError(t, k.SetParams(testCtx.Ctx, types.DefaultParams()))
	return testCtx.Ctx, k, keeper.NewMsgServerImpl(k), bank, ident
}

func TestTransfer_NoPenalty_FullAmountAndAgeTravels(t *testing.T) {
	ctx, k, msg, bank := setupCoin(t)
	now := time.Unix(1_000_000, 0)
	ctx = ctx.WithBlockTime(now)

	alice := sdk.AccAddress([]byte("alice_______________"))
	bob := sdk.AccAddress([]byte("bob_________________"))

	acquiredAt := now.Add(-3 * 24 * time.Hour).Unix()
	bank.bal[alice.String()] = math.NewInt(100_000)
	k.AddCoins(ctx, alice.String(), math.NewInt(100_000), acquiredAt)

	res, err := msg.Transfer(ctx, &types.MsgTransfer{From: alice.String(), To: bob.String(), Amount: "10000"})
	require.NoError(t, err)
	require.Equal(t, "0", res.Burned, "peer-to-peer transfer must not burn")
	require.Equal(t, math.NewInt(10_000), bank.get(bob.String()), "recipient must receive the full amount")
	require.True(t, bank.get(types.FeeCollectorName).IsZero(), "no fee goes to the fee collector")

	bobLots := k.GetCoinAge(ctx, bob.String()).Lots
	require.Len(t, bobLots, 1)
	require.Equal(t, "10000", bobLots[0].Amount)
	require.Equal(t, acquiredAt, bobLots[0].AcquiredAt, "a transfer must not reset the coin's age")
}

func TestEarlyRedeemPenalty_TieredByAge(t *testing.T) {
	ctx, k, _, _ := setupCoin(t)
	now := time.Unix(1_000_000, 0)
	ctx = ctx.WithBlockTime(now)

	young := sdk.AccAddress([]byte("young_seller________")).String()
	k.AddCoins(ctx, young, math.NewInt(100_000), now.Unix())
	feeYoung := k.EarlyRedeemPenalty(ctx, young, math.NewInt(10_000))
	require.Equal(t, math.NewInt(100), feeYoung, "young coin redemption: 1% penalty = 100 uphi")

	old := sdk.AccAddress([]byte("old_seller__________")).String()
	k.AddCoins(ctx, old, math.NewInt(100_000), now.Add(-30*24*time.Hour).Unix())
	feeOld := k.EarlyRedeemPenalty(ctx, old, math.NewInt(10_000))
	require.Equal(t, math.NewInt(20), feeOld, "old coin redemption: 0.2% penalty = 20 uphi")
}

func TestComputeRequiredFee_AndMicroExempt(t *testing.T) {
	ctx, k, _, _, ident := setupCoinIdent(t)
	payer := sdk.AccAddress([]byte("payer_______________"))
	ident.identify(payer.String(), "did:phi:payer")

	bigTransfer := &types.MsgTransfer{From: payer.String(), To: payer.String(), Amount: "100000"}
	require.Equal(t, math.NewInt(5_000), k.ComputeRequiredFee(ctx, []sdk.Msg{bigTransfer}))

	micro := &types.MsgTransfer{From: payer.String(), To: payer.String(), Amount: "1000"}
	require.True(t, k.IsMicroExempt(ctx, payer.String(), []sdk.Msg{micro}))

	require.False(t, k.IsMicroExempt(ctx, payer.String(), []sdk.Msg{bigTransfer}))
}

// TestMicroExempt_IsReadOnly_AndConsumeRespectsQuota guards the simulate/CheckTx bug: IsMicroExempt must be a pure read (so gas-estimation and mempool re-checks never burn the quota), and only ConsumeMicroExemption advances the per-DID daily counter up to the quota.
func TestMicroExempt_IsReadOnly_AndConsumeRespectsQuota(t *testing.T) {
	ctx, k, _, _, ident := setupCoinIdent(t)
	payer := sdk.AccAddress([]byte("micro_payer_________")).String()
	ident.identify(payer, "did:phi:micro-payer")
	micro := []sdk.Msg{&types.MsgTransfer{From: payer, To: payer, Amount: "1000"}}

	for i := 0; i < 100; i++ {
		require.True(t, k.IsMicroExempt(ctx, payer, micro))
	}

	for i := 0; i < types.DefaultMicroDailyQuota; i++ {
		require.True(t, k.IsMicroExempt(ctx, payer, micro), "exempt before the quota is spent")
		k.ConsumeMicroExemption(ctx, payer)
	}

	require.False(t, k.IsMicroExempt(ctx, payer, micro))
}

// TestMicroExempt_RejectsBundledTransfers covers the case where bundling several sub-threshold transfers into one tx must not ride a single quota decrement and go free — only a single-transfer tx qualifies.
func TestMicroExempt_RejectsBundledTransfers(t *testing.T) {
	ctx, k, _, _, ident := setupCoinIdent(t)
	payer := sdk.AccAddress([]byte("bundler_____________")).String()
	ident.identify(payer, "did:phi:bundler")
	one := &types.MsgTransfer{From: payer, To: payer, Amount: "1000"} // sub-threshold
	two := &types.MsgTransfer{From: payer, To: payer, Amount: "2000"} // sub-threshold

	require.True(t, k.IsMicroExempt(ctx, payer, []sdk.Msg{one}))
	require.False(t, k.IsMicroExempt(ctx, payer, []sdk.Msg{one, two}),
		"bundled sub-threshold transfers must not be exempt under one quota slot")
}

// TestPruneMicroQuota_RemovesOldDays covers the case where pruning removes daily micro-quota keys older than the retention window and keeps current ones.
func TestPruneMicroQuota_RemovesOldDays(t *testing.T) {
	ctx, k, _, _ := setupCoin(t)
	ctx = ctx.WithBlockTime(time.Unix(2_000_000_000, 0))
	payer := sdk.AccAddress([]byte("quota_payer_________")).String()
	nowDay := ctx.BlockTime().Unix() / 86400
	oldDay := nowDay - types.MicroQuotaRetentionDays - 1
	k.IncrMicroUsed(ctx, oldDay, payer)
	k.IncrMicroUsed(ctx, nowDay, payer)

	k.PruneMicroQuota(ctx)

	require.Equal(t, uint64(0), k.GetMicroUsed(ctx, oldDay, payer), "a stale day must be pruned")
	require.Equal(t, uint64(1), k.GetMicroUsed(ctx, nowDay, payer), "the current day must be retained")
}

// Coin genesis Validate must reject a malformed lot queue, so the FIFO penalty math never reads garbage state: a bad address, a negative amount or timestamp, an out-of-order queue (which would make the "oldest" lot not actually the oldest), or a queue over the governed bound.
func TestGenesisValidate_CoinAgeLots(t *testing.T) {
	addr := sdk.AccAddress([]byte("coinage_owner_______")).String()
	good := types.GenesisState{Params: types.DefaultParams(), CoinAges: []types.CoinAge{
		{Address: addr, Lots: []types.CoinLot{{Amount: "100", AcquiredAt: 5}, {Amount: "50", AcquiredAt: 9}}},
	}}
	require.NoError(t, good.Validate())

	for name, ca := range map[string]types.CoinAge{
		"bad address":          {Address: "not-bech32", Lots: []types.CoinLot{{Amount: "1"}}},
		"negative amount":      {Address: addr, Lots: []types.CoinLot{{Amount: "-1"}}},
		"unparsable amount":    {Address: addr, Lots: []types.CoinLot{{Amount: "abc"}}},
		"negative acquired_at": {Address: addr, Lots: []types.CoinLot{{Amount: "1", AcquiredAt: -1}}},
		"out of order": {Address: addr, Lots: []types.CoinLot{
			{Amount: "1", AcquiredAt: 100}, {Amount: "1", AcquiredAt: 50},
		}},
	} {
		gs := types.GenesisState{Params: types.DefaultParams(), CoinAges: []types.CoinAge{ca}}
		require.Error(t, gs.Validate(), "must reject: %s", name)
	}

	over := types.GenesisState{Params: types.DefaultParams()}
	over.Params.MaxCoinAgeLots = 2
	over.CoinAges = []types.CoinAge{{Address: addr, Lots: []types.CoinLot{
		{Amount: "1", AcquiredAt: 1}, {Amount: "1", AcquiredAt: 2}, {Amount: "1", AcquiredAt: 3},
	}}}
	require.Error(t, over.Validate(), "a genesis queue over max_coin_age_lots must be rejected")

	dup := types.GenesisState{Params: types.DefaultParams(), CoinAges: []types.CoinAge{{Address: addr}, {Address: addr}}}
	require.Error(t, dup.Validate(), "duplicate coin_age owner must be rejected")
}
