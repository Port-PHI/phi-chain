// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/coin/types"
)

func addrOf(i int) string {
	return sdk.AccAddress([]byte(fmt.Sprintf("attacker-addr-%06d", i))).String()
}

func countCoinAgeRecords(ctx sdk.Context, k interface {
	IterateCoinAges(sdk.Context, func(types.CoinAge) bool)
}) int {
	n := 0
	k.IterateCoinAges(ctx, func(types.CoinAge) bool {
		n++
		return false
	})
	return n
}

// TestMicroExemption_BoundedByDIDsNotAddresses is the pin.
func TestMicroExemption_BoundedByDIDsNotAddresses(t *testing.T) {
	ctx, k, _, _, ident := setupCoinIdent(t)
	ctx = ctx.WithBlockTime(time.Unix(1_700_000_000, 0))

	const attackerAddresses = 500
	for i := 0; i < attackerAddresses; i++ {
		ident.identify(addrOf(i), "did:phi:attacker")
	}

	exempt := 0
	for i := 0; i < attackerAddresses; i++ {
		payer, recipient := addrOf(i), addrOf((i+1)%attackerAddresses)
		micro := []sdk.Msg{&types.MsgTransfer{From: payer, To: recipient, Amount: "1000"}}
		if !k.IsMicroExempt(ctx, payer, micro) {
			continue
		}
		exempt++
		k.ConsumeMicroExemption(ctx, payer)
	}

	require.Equal(t, types.DefaultMicroDailyQuota, exempt,
		"one identity buys one daily quota, however many addresses it spreads across")
	require.Less(t, exempt, attackerAddresses,
		"the exemption count must not scale with the number of addresses")
}

// The same budget spread across many DISTINCT identities scales linearly — which is correct, and is what makes the bound O(DIDs): buying another quota costs another identity, and identities are the thing Phi makes expensive.
func TestMicroExemption_ScalesOnlyWithDistinctIdentities(t *testing.T) {
	for _, humans := range []int{1, 2, 5} {
		t.Run(fmt.Sprintf("%d-humans", humans), func(t *testing.T) {
			ctx, k, _, _, ident := setupCoinIdent(t)
			ctx = ctx.WithBlockTime(time.Unix(1_700_000_000, 0))

			const addressesPerHuman = 10
			for h := 0; h < humans; h++ {
				for a := 0; a < addressesPerHuman; a++ {
					ident.identify(addrOf(h*addressesPerHuman+a), fmt.Sprintf("did:phi:human-%d", h))
				}
			}

			exempt := 0
			for round := 0; round < types.DefaultMicroDailyQuota+5; round++ {
				for i := 0; i < humans*addressesPerHuman; i++ {
					payer, recipient := addrOf(i), addrOf((i+1)%(humans*addressesPerHuman))
					micro := []sdk.Msg{&types.MsgTransfer{From: payer, To: recipient, Amount: "1000"}}
					if !k.IsMicroExempt(ctx, payer, micro) {
						continue
					}
					exempt++
					k.ConsumeMicroExemption(ctx, payer)
				}
			}

			require.Equal(t, humans*types.DefaultMicroDailyQuota, exempt,
				"the total exemption budget must be exactly quota x identities")
		})
	}
}

// A recipient who is not an identified human cannot receive an exempt transfer.
func TestMicroExemption_RecipientMustBeIdentified(t *testing.T) {
	ctx, k, _, _, ident := setupCoinIdent(t)
	ctx = ctx.WithBlockTime(time.Unix(1_700_000_000, 0))

	payer := addrOf(0)
	ident.identify(payer, "did:phi:payer")

	toStranger := []sdk.Msg{&types.MsgTransfer{From: payer, To: addrOf(1), Amount: "1000"}}
	require.False(t, k.IsMicroExempt(ctx, payer, toStranger),
		"an exempt transfer must not be able to open permanent state at an unidentified address")

	ident.identify(addrOf(1), "did:phi:friend")
	require.True(t, k.IsMicroExempt(ctx, payer, toStranger),
		"between two identified humans the exemption applies")
}

// The state bound, measured.
func TestMicroExemption_PermanentCoinAgeIsBoundedByIdentities(t *testing.T) {
	ctx, k, msg, bank, ident := setupCoinIdent(t)
	now := time.Unix(1_700_000_000, 0)
	ctx = ctx.WithBlockTime(now)

	const hops = 300
	for i := 0; i < hops; i++ {
		ident.identify(addrOf(i), "did:phi:attacker")
	}
	stranger := func(i int) string {
		return sdk.AccAddress([]byte(fmt.Sprintf("stranger-addr-%06d", i))).String()
	}

	bank.bal[addrOf(0)] = math.NewInt(1_000_000)
	k.AddCoins(ctx, addrOf(0), math.NewInt(1_000_000), now.Unix())

	exempt := 0
	for i := 0; i < hops; i++ {
		payer := addrOf(0)
		micro := []sdk.Msg{&types.MsgTransfer{From: payer, To: stranger(i), Amount: "1000"}}
		if !k.IsMicroExempt(ctx, payer, micro) {
			continue
		}
		exempt++
		k.ConsumeMicroExemption(ctx, payer)
		_, err := msg.Transfer(ctx, micro[0].(*types.MsgTransfer))
		require.NoError(t, err)
	}

	require.Zero(t, exempt, "dust fan-out to unidentified addresses buys no exemption at all")
	require.Equal(t, 1, countCoinAgeRecords(ctx, k),
		"no permanent coin-age record may be opened by the exempt path beyond the attacker's own")
}

// Coin age still travels on every transfer, exempt or not.
func TestMicroExemption_DoesNotLaunderCoinAge(t *testing.T) {
	ctx, k, msg, bank, ident := setupCoinIdent(t)
	now := time.Unix(1_700_000_000, 0)
	ctx = ctx.WithBlockTime(now)

	alice, bob := addrOf(0), addrOf(1)
	ident.identify(alice, "did:phi:alice")
	ident.identify(bob, "did:phi:bob")

	bank.bal[alice] = math.NewInt(100_000)
	k.AddCoins(ctx, alice, math.NewInt(100_000), now.Unix()) // young coin, acquired now

	micro := &types.MsgTransfer{From: alice, To: bob, Amount: "1000"}
	require.True(t, k.IsMicroExempt(ctx, alice, []sdk.Msg{micro}))
	_, err := msg.Transfer(ctx, micro)
	require.NoError(t, err)

	lots := k.GetCoinAge(ctx, bob).Lots
	require.Len(t, lots, 1, "an exempt transfer must still record the recipient's lot")
	require.Equal(t, now.Unix(), lots[0].AcquiredAt,
		"the coin's real age must travel: a lot skipped here would be repriced as old coin")
}
