// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	"github.com/Port-PHI/phi-chain/x/institutions/keeper"
	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

var baseTime = time.Unix(1_700_000_000, 0).UTC()

func kvStore(f fixture) storetypes.KVStore { return f.ctx.KVStore(f.key) }

func countPrefix(store storetypes.KVStore, prefix []byte) int {
	it := storetypes.KVStorePrefixIterator(store, prefix)
	defer it.Close()
	n := 0
	for ; it.Valid(); it.Next() {
		n++
	}
	return n
}

func dumpStore(store storetypes.KVStore) []string {
	it := store.Iterator(nil, nil)
	defer it.Close()
	var out []string
	for ; it.Valid(); it.Next() {
		out = append(out, fmt.Sprintf("%x=%x", it.Key(), it.Value()))
	}
	return out
}

func acct(i int) sdk.AccAddress {
	b := make([]byte, 20)
	binary.BigEndian.PutUint64(b[12:], uint64(i))
	return sdk.AccAddress(b)
}

func seedCounter(f fixture, instID, kind string, day int64, addr sdk.AccAddress, val string) {
	kvStore(f).Set(types.CounterUserKey(instID, kind, day, addr), []byte(val))
}

func seedRedeemSubject(f fixture, day int64, subject, val string) {
	kvStore(f).Set(types.RedeemSubjectCounterKey(day, types.RedeemSubjectDID, subject), []byte(val))
}

func dayIndexOf(ctx sdk.Context) int64 { return ctx.BlockTime().Unix() / 86400 }

// A same-day sweep must leave current-day counters intact so the in-progress cap still enforces.
func TestPrune_SameDayCapStillEnforced(t *testing.T) {
	f := setup(t)
	f.ctx = f.ctx.WithBlockTime(baseTime)
	f.registerAndAttest(t, "bank-a", 1_000_000)

	_, err := f.msg.UpdateInstitutionParams(f.ctx, &types.MsgUpdateInstitutionParams{
		Signer: f.oper.String(), Institution: "bank-a", Params: types.InstitutionParams{Caps: types.Caps{MintPerUser: "500"}},
	})
	require.NoError(t, err)

	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "300", DepositRef: "dep-1",
	})
	require.NoError(t, err)

	today := dayIndexOf(f.ctx)
	require.True(t, kvStore(f).Has(types.CounterUserKey("bank-a", "mu", today, f.holder)), "today's counter exists")

	f.k.PruneStaleCounters(f.ctx)

	require.True(t, kvStore(f).Has(types.CounterUserKey("bank-a", "mu", today, f.holder)),
		"current-day counter must NOT be pruned")
	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "300", DepositRef: "dep-2",
	})
	require.ErrorIs(t, err, types.ErrCapExceeded, "300+300 > 500: the same-day cap must still bite after a sweep")

	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "200", DepositRef: "dep-3",
	})
	require.NoError(t, err)

	_, broken := keeper.SolvencyInvariant(f.k)(f.ctx)
	require.False(t, broken, "prune moves no value; solvency holds")
}

// Same property for the network-wide per-DID (0x80) family.
func TestPrune_SameDayPerDIDCapStillEnforced(t *testing.T) {
	holder := sdk.AccAddress([]byte("holder______________"))
	f := setupDIDCap(t, capUphiForTest, map[string]string{holder.String(): "did:phi:alice"})
	f.registerAndAttest(t, "bank-a", 100_000)
	f.mintTo(t, "bank-a", holder, "5000", "dep-1")

	require.NoError(t, f.redeem("bank-a", holder, "800", "red-1")) // 8,000 uphi of a 10,000 cap

	f.k.PruneStaleCounters(f.ctx) // same day

	require.ErrorIs(t, f.redeem("bank-a", holder, "800", "red-2"), types.ErrCapExceeded)
	require.NoError(t, f.redeem("bank-a", holder, "200", "red-3")) // exactly on the cap
}

func TestPrune_PastDayDeletedCurrentDayKept(t *testing.T) {
	f := setup(t)
	f.ctx = f.ctx.WithBlockTime(baseTime)
	today := dayIndexOf(f.ctx)
	old := today - 5 // strictly older than the retention window

	seedCounter(f, "bank-a", "mu", old, f.holder, "111")
	seedCounter(f, "bank-a", "rd", old, f.holder, "222")
	seedCounter(f, "bank-a", "mu", today, f.holder, "333")
	seedRedeemSubject(f, old, "did:phi:alice", "444")
	seedRedeemSubject(f, today, "did:phi:alice", "555")

	f.k.PruneStaleCounters(f.ctx)

	store := kvStore(f)
	require.False(t, store.Has(types.CounterUserKey("bank-a", "mu", old, f.holder)), "old 0x40 key pruned")
	require.False(t, store.Has(types.CounterUserKey("bank-a", "rd", old, f.holder)), "old 0x40 key pruned")
	require.False(t, store.Has(types.RedeemSubjectCounterKey(old, types.RedeemSubjectDID, "did:phi:alice")), "old 0x80 key pruned")

	require.True(t, store.Has(types.CounterUserKey("bank-a", "mu", today, f.holder)), "current 0x40 key kept")
	require.True(t, store.Has(types.RedeemSubjectCounterKey(today, types.RedeemSubjectDID, "did:phi:alice")), "current 0x80 key kept")
}

// A day within the retention buffer is kept; only days strictly before staleBefore are removed.
func TestPrune_RetentionBufferKeepsRecentDays(t *testing.T) {
	f := setup(t)
	f.ctx = f.ctx.WithBlockTime(baseTime)
	today := dayIndexOf(f.ctx)

	seedCounter(f, "bank-a", "mu", today-1, f.holder, "1") // within buffer (staleBefore = today-2)
	seedCounter(f, "bank-a", "mu", today-3, f.holder, "1") // beyond buffer

	f.k.PruneStaleCounters(f.ctx)

	require.True(t, kvStore(f).Has(types.CounterUserKey("bank-a", "mu", today-1, f.holder)), "yesterday kept by buffer")
	require.False(t, kvStore(f).Has(types.CounterUserKey("bank-a", "mu", today-3, f.holder)), "day beyond buffer pruned")
}

func TestPrune_EmergencyCounterSurvives(t *testing.T) {
	f := setup(t)
	f.ctx = f.ctx.WithBlockTime(baseTime)
	today := dayIndexOf(f.ctx)

	startedAt := baseTime.Unix()
	erKey := types.CounterUserKey("bank-a", "er", startedAt, f.holder)
	kvStore(f).Set(erKey, []byte("777"))
	seedCounter(f, "bank-a", "rd", today-5, f.holder, "1")

	for d := int64(0); d < 40; d++ {
		f.k.PruneStaleCounters(f.ctx.WithBlockTime(baseTime.Add(time.Duration(d) * 24 * time.Hour)))
	}

	require.True(t, kvStore(f).Has(erKey), `emergency "er" counter must never be pruned`)
	require.Equal(t, []byte("777"), kvStore(f).Get(erKey), `"er" value untouched`)
	require.False(t, kvStore(f).Has(types.CounterUserKey("bank-a", "rd", today-5, f.holder)), "stale daily counter was pruned (sweep ran)")
}

// An active emergency's cumulative cap is still enforced after a sweep: the "er" bucket is not reset.
func TestPrune_EmergencyEnforcementUnaffected(t *testing.T) {
	f := setup(t)
	f.ctx = f.ctx.WithBlockTime(baseTime)
	f.registerAndAttest(t, "bank-a", 1_000_000_000)
	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "1000000000", DepositRef: "dep-1",
	})
	require.NoError(t, err)
	_, err = f.msg.SetEmergencyRedemption(f.ctx, &types.MsgSetEmergencyRedemption{Authority: f.authority, Active: true})
	require.NoError(t, err)

	require.NoError(t, f.redeemAt(35, "20000000", "red-1"))

	for d := int64(35); d < 45; d++ {
		f.k.PruneStaleCounters(f.ctx.WithBlockTime(f.ctx.BlockTime().Add(time.Duration(d) * 24 * time.Hour)))
	}

	require.ErrorIs(t, f.redeemAt(35, "1", "red-2"), types.ErrRedemptionThrottled,
		"the emergency cumulative cap must still bite after a sweep")
}

func seedDeterministicSet(f fixture, today int64) {
	kinds := []string{"md", "mu", "rd", "ru"}
	for i := 0; i < 300; i++ {
		day := today - int64(i%8) // days [today-7 .. today]
		seedCounter(f, fmt.Sprintf("bank-%d", i%3), kinds[i%4], day, acct(i), fmt.Sprintf("%d", i+1))
		seedRedeemSubject(f, day, fmt.Sprintf("did:phi:%d", i), fmt.Sprintf("%d", i+1))
	}
	kvStore(f).Set(types.CounterUserKey("bank-a", "er", baseTime.Unix(), acct(9999)), []byte("42"))
}

func TestPrune_DeterministicAcrossInstances(t *testing.T) {
	run := func() []string {
		f := setup(t)
		f.ctx = f.ctx.WithBlockTime(baseTime)
		today := dayIndexOf(f.ctx)
		seedDeterministicSet(f, today)
		for d := int64(0); d < 4; d++ {
			ctx := f.ctx.WithBlockTime(baseTime.Add(time.Duration(d) * 24 * time.Hour))
			for b := 0; b < 6; b++ {
				f.k.PruneStaleCounters(ctx)
			}
		}
		return dumpStore(kvStore(f))
	}
	require.Equal(t, run(), run(), "two independent instances must reach byte-identical state (incl. the 0x90 cursor)")
}

func TestPrune_BoundedPerBlockAndDrains(t *testing.T) {
	f := setup(t)
	f.ctx = f.ctx.WithBlockTime(baseTime)
	today := dayIndexOf(f.ctx)
	old := today - 5

	const seeded = types.CounterPruneBudget*2 + 37 // 549: comfortably more than one budget
	for i := 0; i < seeded; i++ {
		seedCounter(f, "bank-a", "ru", old, acct(i), "1")            // 0x40 ring family
		seedRedeemSubject(f, old, fmt.Sprintf("did:phi:%d", i), "1") // 0x80 front family
	}
	require.Equal(t, seeded, countPrefix(kvStore(f), types.CounterPrefix))
	require.Equal(t, seeded, countPrefix(kvStore(f), types.RedeemSubjectPrefix))

	prevCounter, prevSubject := seeded, seeded
	maxCalls := 0
	for prevCounter > 0 || prevSubject > 0 {
		f.k.PruneStaleCounters(f.ctx)
		nowCounter := countPrefix(kvStore(f), types.CounterPrefix)
		nowSubject := countPrefix(kvStore(f), types.RedeemSubjectPrefix)

		require.LessOrEqual(t, prevCounter-nowCounter, types.CounterPruneBudget, "0x40: <= budget deleted per block")
		require.LessOrEqual(t, prevSubject-nowSubject, types.CounterPruneBudget, "0x80: <= budget deleted per block")

		prevCounter, prevSubject = nowCounter, nowSubject
		maxCalls++
		require.Less(t, maxCalls, 20, "must drain in a bounded number of blocks")
	}
	require.Zero(t, countPrefix(kvStore(f), types.CounterPrefix), "0x40 fully drained")
	require.Zero(t, countPrefix(kvStore(f), types.RedeemSubjectPrefix), "0x80 fully drained")

	f2 := setup(t)
	f2.ctx = f2.ctx.WithBlockTime(baseTime)
	for i := 0; i < seeded; i++ {
		seedRedeemSubject(f2, old, fmt.Sprintf("did:phi:%d", i), "1")
	}
	f2.k.PruneStaleCounters(f2.ctx)
	require.Equal(t, seeded-types.CounterPruneBudget, countPrefix(kvStore(f2), types.RedeemSubjectPrefix),
		"exactly budget keys removed on the first block")
}

func TestPrune_GenesisRoundTripExcludesCursor(t *testing.T) {
	f := setup(t)
	f.ctx = f.ctx.WithBlockTime(baseTime)
	f.mintBacked(t, "bank-a", 1000, "dep-1") // writes 0x40 cap counters

	seedCounter(f, "bank-a", "ru", dayIndexOf(f.ctx)-9, acct(1), "1")
	f.k.PruneStaleCounters(f.ctx)
	require.True(t, kvStore(f).Has(types.CounterPruneCursorKey()) || countPrefix(kvStore(f), types.CounterPrefix) > 0,
		"sweep executed")

	exported := f.k.ExportGenesis(f.ctx)
	require.NoError(t, exported.Validate())
	require.NotEmpty(t, exported.CapCounters, "cap counters must be exported")

	for _, e := range exported.CapCounters {
		require.NotEqual(t, types.CounterPruneCursorPrefix[0], e.Key[0], "cursor must not be exported as a cap counter")
	}

	f2 := setup(t)
	f2.ctx = f2.ctx.WithBlockTime(baseTime)
	f2.bank.supply[cointypes.Denom] = math.NewInt(10000)
	f2.k.InitGenesis(f2.ctx, *exported)
	require.False(t, kvStore(f2).Has(types.CounterPruneCursorKey()), "imported state carries no prune cursor")

	require.NotPanics(t, func() { f2.k.PruneStaleCounters(f2.ctx) })
	_, broken := keeper.SolvencyInvariant(f2.k)(f2.ctx)
	require.False(t, broken, "solvency holds after import + sweep")
}

func TestParseCounterKeyDay(t *testing.T) {
	addr := acct(7)
	tot := types.CounterTotalKey("bank-xyz", "rd", 19675)
	kind, day, ok := types.ParseCounterKeyDay(tot)
	require.True(t, ok)
	require.Equal(t, "rd", kind)
	require.Equal(t, int64(19675), day)

	usr := types.CounterUserKey("bank-xyz", "mu", 20000, addr)
	kind, day, ok = types.ParseCounterKeyDay(usr)
	require.True(t, ok)
	require.Equal(t, "mu", kind)
	require.Equal(t, int64(20000), day)

	er := types.CounterUserKey("bank-xyz", "er", baseTime.Unix(), addr)
	kind, _, ok = types.ParseCounterKeyDay(er)
	require.True(t, ok)
	require.False(t, types.IsPrunableCounterKind(kind))

	_, _, ok = types.ParseCounterKeyDay(nil)
	require.False(t, ok)
	_, _, ok = types.ParseCounterKeyDay([]byte{0x40, 0x02})
	require.False(t, ok)
}
