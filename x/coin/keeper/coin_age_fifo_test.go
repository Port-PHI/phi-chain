// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/coin/types"
)

// FIFO ORDERING.
func TestFIFO_RedeemSpendsTheOldestCoinFirst(t *testing.T) {
	ctx, k, _, _ := setupCoin(t)
	now := time.Unix(1_700_000_000, 0)
	ctx = ctx.WithBlockTime(now)

	holder := sdk.AccAddress([]byte("fifo_holder_________")).String()
	k.AddCoins(ctx, holder, math.NewInt(100_000), now.Add(-10*24*time.Hour).Unix()) // OLD
	k.AddCoins(ctx, holder, math.NewInt(100_000), now.Add(-1*24*time.Hour).Unix())  // YOUNG

	require.Equal(t, math.NewInt(100).String(), k.EarlyRedeemPenalty(ctx, holder, math.NewInt(50_000)).String(),
		"the oldest coin is spent first and pays the OLD rate")

	lots := k.GetCoinAge(ctx, holder).Lots
	require.Len(t, lots, 2)
	require.Equal(t, "50000", lots[0].Amount)
	require.Equal(t, now.Add(-10*24*time.Hour).Unix(), lots[0].AcquiredAt, "the remainder is still the oldest coin")

	require.Equal(t, math.NewInt(600).String(), k.EarlyRedeemPenalty(ctx, holder, math.NewInt(100_000)).String(),
		"crossing the lot boundary blends the two lots at their OWN rates")

	lots = k.GetCoinAge(ctx, holder).Lots
	require.Len(t, lots, 1)
	require.Equal(t, "50000", lots[0].Amount, "only fresh coin is left")
	require.Equal(t, now.Add(-1*24*time.Hour).Unix(), lots[0].AcquiredAt)
}

// AGE TRAVELS ON TRANSFER.
func TestFIFO_AgeTravelsWithTheCoinOnTransfer(t *testing.T) {
	ctx, k, msg, bank := setupCoin(t)
	now := time.Unix(1_700_000_000, 0)
	ctx = ctx.WithBlockTime(now)

	alice := sdk.AccAddress([]byte("alice_______________"))
	bob := sdk.AccAddress([]byte("bob_________________"))

	oldAt := now.Add(-30 * 24 * time.Hour).Unix()
	bank.bal[alice.String()] = math.NewInt(200_000)
	k.AddCoins(ctx, alice.String(), math.NewInt(100_000), oldAt)
	k.AddCoins(ctx, alice.String(), math.NewInt(100_000), now.Unix())

	_, err := msg.Transfer(ctx, &types.MsgTransfer{From: alice.String(), To: bob.String(), Amount: "100000"})
	require.NoError(t, err)

	bobLots := k.GetCoinAge(ctx, bob.String()).Lots
	require.Len(t, bobLots, 1)
	require.Equal(t, "100000", bobLots[0].Amount)
	require.Equal(t, oldAt, bobLots[0].AcquiredAt, "Bob inherits the coin's real acquisition time")

	require.Equal(t, math.NewInt(200).String(), k.EarlyRedeemPenalty(ctx, bob.String(), math.NewInt(100_000)).String(),
		"the recipient is penalized by the coin's real age, not by receipt time")

	aliceLots := k.GetCoinAge(ctx, alice.String()).Lots
	require.Len(t, aliceLots, 1)
	require.Equal(t, now.Unix(), aliceLots[0].AcquiredAt)
	require.Equal(t, math.NewInt(1_000).String(), k.EarlyRedeemPenalty(ctx, alice.String(), math.NewInt(100_000)).String(),
		"Alice's remaining coin is fresh and pays 1%")
}

// A transfer must not let a holder launder FRESH coin into an old age: the recipient of young coin gets young coin, whatever the recipient's own queue looks like.
func TestFIFO_TransferCannotLaunderYoungCoinIntoOld(t *testing.T) {
	ctx, k, msg, bank := setupCoin(t)
	now := time.Unix(1_700_000_000, 0)
	ctx = ctx.WithBlockTime(now)

	alice := sdk.AccAddress([]byte("alice_______________"))
	bob := sdk.AccAddress([]byte("bob_________________"))

	bank.bal[alice.String()] = math.NewInt(100_000)
	k.AddCoins(ctx, alice.String(), math.NewInt(100_000), now.Unix())
	k.AddCoins(ctx, bob.String(), math.NewInt(500_000), now.Add(-30*24*time.Hour).Unix())

	_, err := msg.Transfer(ctx, &types.MsgTransfer{From: alice.String(), To: bob.String(), Amount: "100000"})
	require.NoError(t, err)

	lots := k.GetCoinAge(ctx, bob.String()).Lots
	require.Len(t, lots, 2)
	require.Equal(t, "500000", lots[0].Amount)
	require.Equal(t, "100000", lots[1].Amount)
	require.Equal(t, now.Unix(), lots[1].AcquiredAt, "received fresh coin stays fresh")

	require.Equal(t, math.NewInt(2_000).String(), k.EarlyRedeemPenalty(ctx, bob.String(), math.NewInt(600_000)).String())
}

// ANTI-DILUTION, end to end through the keeper: an attacker dust-spams a victim with fresh coin to try to drag the victim's aged balance into the young tier.
func TestFIFO_DustSpamCannotDiluteAVictimsAgedCoin(t *testing.T) {
	ctx, k, msg, bank := setupCoin(t)
	now := time.Unix(1_700_000_000, 0)
	ctx = ctx.WithBlockTime(now)

	params := types.DefaultParams()
	params.MaxCoinAgeLots = 4
	require.NoError(t, k.SetParams(ctx, params))

	victim := sdk.AccAddress([]byte("victim______________"))
	attacker := sdk.AccAddress([]byte("attacker____________"))

	agedAt := now.Add(-30 * 24 * time.Hour).Unix()
	k.AddCoins(ctx, victim.String(), math.NewInt(1_000_000), agedAt)

	bank.bal[attacker.String()] = math.NewInt(30)
	k.AddCoins(ctx, attacker.String(), math.NewInt(30), now.Unix())
	for i := 0; i < 30; i++ {
		_, err := msg.Transfer(ctx, &types.MsgTransfer{
			From: attacker.String(), To: victim.String(), Amount: "1",
		})
		require.NoError(t, err)
		require.LessOrEqual(t, len(k.GetCoinAge(ctx, victim.String()).Lots), 4,
			"the victim's queue stays bounded under the flood")
	}

	lots := k.GetCoinAge(ctx, victim.String()).Lots
	drift := lots[0].AcquiredAt - agedAt
	require.GreaterOrEqual(t, drift, int64(0))
	require.Less(t, drift, int64(120), "dust carries almost no weight in the merged age")

	require.Equal(t, math.NewInt(2_000).String(),
		k.EarlyRedeemPenalty(ctx, victim.String(), math.NewInt(1_000_000)).String(),
		"the aged coin still redeems at 0.2% after being dusted")
}

// The bound holds under a self-inflicted flood too: a holder's own deposits can never grow their queue past max_coin_age_lots, so a redemption's queue walk is O(1) in the number of deposits.
func TestFIFO_QueueStaysBounded(t *testing.T) {
	ctx, k, _, _ := setupCoin(t)
	now := time.Unix(1_700_000_000, 0)
	ctx = ctx.WithBlockTime(now)

	holder := sdk.AccAddress([]byte("spammer_____________")).String()
	total := math.ZeroInt()
	for i := 0; i < 500; i++ {
		ctx = ctx.WithBlockTime(now.Add(time.Duration(i) * time.Minute))
		k.AddCoins(ctx, holder, math.NewInt(100), ctx.BlockTime().Unix())
		total = total.Add(math.NewInt(100))
		require.LessOrEqual(t, uint32(len(k.GetCoinAge(ctx, holder).Lots)), types.DefaultMaxCoinAgeLots)
	}

	require.Equal(t, total.String(), types.TotalLots(k.GetCoinAge(ctx, holder).Lots).String(),
		"500 deposits collapsed into at most 64 lots, with no uphi lost")
}

// The lot queue round-trips through genesis, and a fully redeemed holder leaves no residue.
func TestFIFO_GenesisRoundTripAndEmptyQueueIsPruned(t *testing.T) {
	ctx, k, _, _ := setupCoin(t)
	now := time.Unix(1_700_000_000, 0)
	ctx = ctx.WithBlockTime(now)

	holder := sdk.AccAddress([]byte("genesis_holder______")).String()
	gs := types.DefaultGenesis()
	gs.CoinAges = []types.CoinAge{{Address: holder, Lots: []types.CoinLot{
		{Amount: "100", AcquiredAt: now.Add(-20 * 24 * time.Hour).Unix()},
		{Amount: "200", AcquiredAt: now.Add(-2 * 24 * time.Hour).Unix()},
	}}}
	require.NoError(t, gs.Validate())

	k.InitGenesis(ctx, *gs)
	got := k.ExportGenesis(ctx)
	require.Equal(t, gs.Params, got.Params)
	require.Equal(t, gs.CoinAges, got.CoinAges, "the lot queue must survive the round-trip, order intact")

	require.Equal(t, math.NewInt(0).String(), k.EarlyRedeemPenalty(ctx, holder, math.NewInt(100)).String(),
		"100 uphi at 0.2% floors to 0 — the floor is per lot and favours the holder")

	k.EarlyRedeemPenalty(ctx, holder, math.NewInt(200))
	require.Empty(t, k.GetCoinAge(ctx, holder).Lots, "a fully redeemed holder leaves no lot queue behind")
}
