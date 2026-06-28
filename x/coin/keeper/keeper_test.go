// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"context"
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
func (b *fakeBank) GetBalance(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewCoin(denom, b.get(addr.String()))
}
func (b *fakeBank) GetSupply(_ context.Context, denom string) sdk.Coin {
	return sdk.NewCoin(denom, math.ZeroInt())
}

func setupCoin(t *testing.T) (sdk.Context, keeper.Keeper, types.MsgServer, *fakeBank) {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_coin"))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	authority := sdk.AccAddress([]byte("gov_authority_______")).String()
	bank := newFakeBank()
	k := keeper.NewKeeper(cdc, key, authority, bank)
	require.NoError(t, k.SetParams(testCtx.Ctx, types.DefaultParams()))
	return testCtx.Ctx, k, keeper.NewMsgServerImpl(k), bank
}

func TestTransfer_NoDemurrage_FullAmountAndAgePreserved(t *testing.T) {
	ctx, k, msg, bank := setupCoin(t)
	now := time.Unix(1_000_000, 0)
	ctx = ctx.WithBlockTime(now)

	alice := sdk.AccAddress([]byte("alice_______________"))
	bob := sdk.AccAddress([]byte("bob_________________"))

	// alice has 100,000 uphi of young coins (from 3 days ago).
	youngSince := now.Add(-3 * 24 * time.Hour).Unix()
	bank.bal[alice.String()] = math.NewInt(100_000)
	k.AddYoungCoins(ctx, alice.String(), math.NewInt(100_000), youngSince)

	// Transfer 10,000 - there must be no burn; the recipient receives the full amount.
	res, err := msg.Transfer(ctx, &types.MsgTransfer{From: alice.String(), To: bob.String(), Amount: "10000"})
	require.NoError(t, err)
	require.Equal(t, "0", res.Burned, "peer-to-peer transfer must not burn")
	require.Equal(t, math.NewInt(10_000), bank.get(bob.String()), "recipient must receive the full amount")
	require.True(t, bank.get(types.FeeCollectorName).IsZero(), "no fee goes to the fee collector")

	// Anti-circumvention: bob's coins must also be "young" with alice's same age (age not reset).
	bobCA := keeper.MatureCoinAge(k.GetCoinAge(ctx, bob.String()), now.Unix(), k.GetParams(ctx).CoinAgeThresholdSeconds)
	require.Equal(t, "10000", bobCA.YoungAmount, "transferred coins must remain young")
	require.Equal(t, youngSince, bobCA.YoungSince, "coin age is not reset by transfer")
}

func TestRedeemDemurrage_TieredByAge(t *testing.T) {
	ctx, k, _, _ := setupCoin(t)
	now := time.Unix(1_000_000, 0)
	ctx = ctx.WithBlockTime(now)

	// Young seller: 1% penalty.
	young := sdk.AccAddress([]byte("young_seller________")).String()
	k.AddYoungCoins(ctx, young, math.NewInt(100_000), now.Unix())
	feeYoung := k.RedeemDemurrage(ctx, young, math.NewInt(10_000))
	require.Equal(t, math.NewInt(100), feeYoung, "young coin redemption: 1% penalty = 100 uphi")

	// Old seller: 0.2% penalty.
	old := sdk.AccAddress([]byte("old_seller__________")).String()
	k.AddYoungCoins(ctx, old, math.NewInt(100_000), now.Add(-30*24*time.Hour).Unix())
	feeOld := k.RedeemDemurrage(ctx, old, math.NewInt(10_000))
	require.Equal(t, math.NewInt(20), feeOld, "old coin redemption: 0.2% penalty = 20 uphi")
}

func TestComputeRequiredFee_AndMicroExempt(t *testing.T) {
	ctx, k, _, _ := setupCoin(t)
	payer := sdk.AccAddress([]byte("payer_______________"))

	// Fixed transfer fee = 5,000 uphi (0.005 PHI).
	bigTransfer := &types.MsgTransfer{From: payer.String(), To: payer.String(), Amount: "100000"}
	require.Equal(t, math.NewInt(5_000), k.ComputeRequiredFee(ctx, []sdk.Msg{bigTransfer}))

	// Micro transaction (below 50,000 uphi) with daily quota -> exempt.
	micro := &types.MsgTransfer{From: payer.String(), To: payer.String(), Amount: "1000"}
	require.True(t, k.IsMicroExempt(ctx, payer.String(), []sdk.Msg{micro}))

	// A large transfer is not exempt.
	require.False(t, k.IsMicroExempt(ctx, payer.String(), []sdk.Msg{bigTransfer}))
}

// TestMicroExempt_IsReadOnly_AndConsumeRespectsQuota guards the simulate/CheckTx bug: IsMicroExempt
// must be a pure read (so gas-estimation and mempool re-checks never burn the quota), and only
// ConsumeMicroExemption advances the per-DID daily counter up to the quota.
func TestMicroExempt_IsReadOnly_AndConsumeRespectsQuota(t *testing.T) {
	ctx, k, _, _ := setupCoin(t)
	payer := sdk.AccAddress([]byte("micro_payer_________")).String()
	micro := []sdk.Msg{&types.MsgTransfer{From: payer, To: payer, Amount: "1000"}}

	// Pure read: calling IsMicroExempt many times never consumes the quota.
	for i := 0; i < 100; i++ {
		require.True(t, k.IsMicroExempt(ctx, payer, micro))
	}

	// Only ConsumeMicroExemption advances the counter; it stays exempt up to the daily quota.
	for i := 0; i < types.DefaultMicroDailyQuota; i++ {
		require.True(t, k.IsMicroExempt(ctx, payer, micro), "exempt before the quota is spent")
		k.ConsumeMicroExemption(ctx, payer)
	}

	// Daily quota now exhausted -> no longer exempt.
	require.False(t, k.IsMicroExempt(ctx, payer, micro))
}

// TestMicroExempt_RejectsBundledTransfers covers the case where bundling several sub-threshold transfers
// into one tx must not ride a single quota decrement and go free — only a single-transfer tx qualifies.
func TestMicroExempt_RejectsBundledTransfers(t *testing.T) {
	ctx, k, _, _ := setupCoin(t)
	payer := sdk.AccAddress([]byte("bundler_____________")).String()
	one := &types.MsgTransfer{From: payer, To: payer, Amount: "1000"} // sub-threshold
	two := &types.MsgTransfer{From: payer, To: payer, Amount: "2000"} // sub-threshold

	// A single sub-threshold transfer is exempt.
	require.True(t, k.IsMicroExempt(ctx, payer, []sdk.Msg{one}))
	// Two sub-threshold transfers bundled in one tx are NOT exempt.
	require.False(t, k.IsMicroExempt(ctx, payer, []sdk.Msg{one, two}),
		"bundled sub-threshold transfers must not be exempt under one quota slot")
}

// TestPruneMicroQuota_RemovesOldDays covers the case where pruning removes daily micro-quota keys older
// than the retention window and keeps current ones.
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

// Coin genesis Validate must reject a malformed CoinAge (bad address, negative bucket, or
// negative young_since) so the demurrage math never reads garbage state.
func TestGenesisValidate_CoinAge(t *testing.T) {
	addr := sdk.AccAddress([]byte("coinage_owner_______")).String()
	good := types.GenesisState{Params: types.DefaultParams(), CoinAges: []types.CoinAge{
		{Address: addr, YoungAmount: "100", OldAmount: "0", YoungSince: 5},
	}}
	require.NoError(t, good.Validate())

	for name, ca := range map[string]types.CoinAge{
		"bad address":          {Address: "not-bech32", YoungAmount: "1"},
		"negative young":       {Address: addr, YoungAmount: "-1"},
		"negative old":         {Address: addr, OldAmount: "-1"},
		"negative young_since": {Address: addr, YoungSince: -1},
	} {
		gs := types.GenesisState{Params: types.DefaultParams(), CoinAges: []types.CoinAge{ca}}
		require.Error(t, gs.Validate(), "must reject: %s", name)
	}

	// Duplicate owner rejected.
	dup := types.GenesisState{Params: types.DefaultParams(), CoinAges: []types.CoinAge{{Address: addr}, {Address: addr}}}
	require.Error(t, dup.Validate(), "duplicate coin_age owner must be rejected")
}
