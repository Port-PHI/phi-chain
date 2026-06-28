// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"cosmossdk.io/log"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/coin/types"
)

// Keeper manages the coin module's state.
type Keeper struct {
	cdc        codec.BinaryCodec
	storeKey   storetypes.StoreKey
	authority  string
	bankKeeper types.BankKeeper
}

// NewKeeper creates a new keeper.
func NewKeeper(cdc codec.BinaryCodec, storeKey storetypes.StoreKey, authority string, bank types.BankKeeper) Keeper {
	return Keeper{cdc: cdc, storeKey: storeKey, authority: authority, bankKeeper: bank}
}

// GetAuthority returns the governance authority address.
func (k Keeper) GetAuthority() string { return k.authority }

// Logger returns the module logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}

// BankKeeper exposes access to the bank keeper (for the ante handler).
func (k Keeper) BankKeeper() types.BankKeeper { return k.bankKeeper }

// --- Parameters ---

// GetParams returns the current parameters.
func (k Keeper) GetParams(ctx sdk.Context) (p types.Params) {
	bz := ctx.KVStore(k.storeKey).Get(types.ParamsKey)
	if bz == nil {
		return types.DefaultParams()
	}
	k.cdc.MustUnmarshal(bz, &p)
	return p
}

// SetParams stores the parameters.
func (k Keeper) SetParams(ctx sdk.Context, p types.Params) error {
	if err := p.Validate(); err != nil {
		return err
	}
	ctx.KVStore(k.storeKey).Set(types.ParamsKey, k.cdc.MustMarshal(&p))
	return nil
}

// --- Coin age (CoinAge) ---

// GetCoinAge reads an address's coin-age buckets (zero if absent).
func (k Keeper) GetCoinAge(ctx sdk.Context, address string) types.CoinAge {
	bz := ctx.KVStore(k.storeKey).Get(types.CoinAgeKey(address))
	if bz == nil {
		return types.CoinAge{Address: address, YoungAmount: "0", OldAmount: "0", YoungSince: ctx.BlockTime().Unix()}
	}
	var ca types.CoinAge
	k.cdc.MustUnmarshal(bz, &ca)
	return ca
}

// SetCoinAge stores the coin-age buckets.
func (k Keeper) SetCoinAge(ctx sdk.Context, ca types.CoinAge) {
	ctx.KVStore(k.storeKey).Set(types.CoinAgeKey(ca.Address), k.cdc.MustMarshal(&ca))
}

// IterateCoinAges iterates over all coin-age buckets.
func (k Keeper) IterateCoinAges(ctx sdk.Context, cb func(types.CoinAge) bool) {
	store := ctx.KVStore(k.storeKey)
	it := storetypes.KVStorePrefixIterator(store, types.CoinAgePrefix)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var ca types.CoinAge
		k.cdc.MustUnmarshal(it.Value(), &ca)
		if cb(ca) {
			break
		}
	}
}

// MatureCoinAge moves the young bucket into the old bucket once it reaches the threshold.
// Pure function (does not mutate state); the caller stores the result.
func MatureCoinAge(ca types.CoinAge, now, thresholdSeconds int64) types.CoinAge {
	young := mustInt(ca.YoungAmount)
	if young.IsPositive() && now-ca.YoungSince >= thresholdSeconds {
		old := mustInt(ca.OldAmount).Add(young)
		ca.OldAmount = old.String()
		ca.YoungAmount = "0"
		ca.YoungSince = now
	}
	return ca
}

// AddYoungCoins adds a newly minted/received amount to an address's young bucket.
// (Institution mint and transfer both use this.)
func (k Keeper) AddYoungCoins(ctx sdk.Context, address string, amount math.Int, youngSince int64) {
	ca := MatureCoinAge(k.GetCoinAge(ctx, address), ctx.BlockTime().Unix(), k.GetParams(ctx).CoinAgeThresholdSeconds)
	young := mustInt(ca.YoungAmount)
	// Preserve age: keep the oldest timestamp (prevents rejuvenating coins via transfer).
	if young.IsZero() || youngSince < ca.YoungSince {
		ca.YoungSince = youngSince
	}
	ca.YoungAmount = young.Add(amount).String()
	k.SetCoinAge(ctx, ca)
}

// RedeemDemurrage computes the tiered coin-age exit penalty when redeeming `redeemUphi`
// and decrements the seller's buckets by the same amount. The result is in uphi.
// penalty = young*young_burn_bps + old*old_burn_bps (proportional to the seller's coin-age mix).
// Note: this penalty is deducted only from the toman the user is paid; the PHI itself is fully burned
// and the vault is fully decremented (so the solvency invariant stays intact).
func (k Keeper) RedeemDemurrage(ctx sdk.Context, address string, redeemUphi math.Int) math.Int {
	params := k.GetParams(ctx)
	ca := MatureCoinAge(k.GetCoinAge(ctx, address), ctx.BlockTime().Unix(), params.CoinAgeThresholdSeconds)
	young := mustInt(ca.YoungAmount)
	old := mustInt(ca.OldAmount)
	tracked := young.Add(old)
	// Untracked coins are assumed old (in the user's favor - lowest penalty).
	if tracked.LT(redeemUphi) {
		old = old.Add(redeemUphi.Sub(tracked))
		tracked = young.Add(old)
	}

	youngSpent := math.ZeroInt()
	if tracked.IsPositive() {
		youngSpent = redeemUphi.Mul(young).Quo(tracked)
	}
	oldSpent := redeemUphi.Sub(youngSpent)

	fee := youngSpent.MulRaw(int64(params.YoungBurnBps)).QuoRaw(10_000).
		Add(oldSpent.MulRaw(int64(params.OldBurnBps)).QuoRaw(10_000))

	// Decrement the buckets (the sold coins have left circulation).
	ca.YoungAmount = young.Sub(youngSpent).String()
	ca.OldAmount = old.Sub(oldSpent).String()
	k.SetCoinAge(ctx, ca)

	return fee
}

// --- Daily micro-exemption quota ---

// GetMicroUsed returns how many times an address has used the micro-exemption today.
func (k Keeper) GetMicroUsed(ctx sdk.Context, day int64, address string) uint64 {
	bz := ctx.KVStore(k.storeKey).Get(types.MicroQuotaKey(day, address))
	if bz == nil {
		return 0
	}
	return sdk.BigEndianToUint64(bz)
}

// IncrMicroUsed increments today's usage counter by one.
func (k Keeper) IncrMicroUsed(ctx sdk.Context, day int64, address string) {
	n := k.GetMicroUsed(ctx, day, address) + 1
	ctx.KVStore(k.storeKey).Set(types.MicroQuotaKey(day, address), sdk.Uint64ToBigEndian(n))
}

// PruneMicroQuota deletes daily micro-exemption quota keys older than MicroQuotaRetentionDays (audit
// L-4). MicroQuotaKey is prefix|big-endian(day)|address, so a bounded iterator over
// [prefix, prefix|be(cutoffDay)) visits only expired keys — the scan is O(expired), not O(all keys).
func (k Keeper) PruneMicroQuota(ctx sdk.Context) {
	cutoffDay := ctx.BlockTime().Unix()/86400 - types.MicroQuotaRetentionDays
	if cutoffDay <= 0 {
		return
	}
	store := ctx.KVStore(k.storeKey)
	end := append(append([]byte{}, types.MicroQuotaPrefix...), sdk.Uint64ToBigEndian(uint64(cutoffDay))...)
	it := store.Iterator(types.MicroQuotaPrefix, end)
	defer it.Close()
	var stale [][]byte
	for ; it.Valid(); it.Next() {
		stale = append(stale, append([]byte(nil), it.Key()...))
	}
	for _, key := range stale {
		store.Delete(key)
	}
}

// mustInt converts a string to math.Int; invalid input yields zero.
func mustInt(s string) math.Int {
	if v, ok := math.NewIntFromString(s); ok {
		return v
	}
	return math.ZeroInt()
}
