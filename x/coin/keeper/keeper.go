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
	cdc            codec.BinaryCodec
	storeKey       storetypes.StoreKey
	authority      string
	bankKeeper     types.BankKeeper
	identityKeeper types.IdentityKeeper
}

// NewKeeper creates a new keeper.
func NewKeeper(cdc codec.BinaryCodec, storeKey storetypes.StoreKey, authority string, bank types.BankKeeper, identity types.IdentityKeeper) Keeper {
	return Keeper{cdc: cdc, storeKey: storeKey, authority: authority, bankKeeper: bank, identityKeeper: identity}
}

// GetAuthority returns the governance authority address.
func (k Keeper) GetAuthority() string { return k.authority }

// Logger returns the module logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}

// BankKeeper exposes access to the bank keeper (for the ante handler).
func (k Keeper) BankKeeper() types.BankKeeper { return k.bankKeeper }

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

// GetCoinAge reads an address's coin-age lot queue (empty if absent), ordered oldest-first.
func (k Keeper) GetCoinAge(ctx sdk.Context, address string) types.CoinAge {
	bz := ctx.KVStore(k.storeKey).Get(types.CoinAgeKey(address))
	if bz == nil {
		return types.CoinAge{Address: address}
	}
	var ca types.CoinAge
	k.cdc.MustUnmarshal(bz, &ca)
	return ca
}

// SetCoinAge stores an address's lot queue, dropping it entirely once it is empty so a fully redeemed holder leaves no residue in state.
func (k Keeper) SetCoinAge(ctx sdk.Context, ca types.CoinAge) {
	store := ctx.KVStore(k.storeKey)
	if len(ca.Lots) == 0 {
		store.Delete(types.CoinAgeKey(ca.Address))
		return
	}
	store.Set(types.CoinAgeKey(ca.Address), k.cdc.MustMarshal(&ca))
}

// IterateCoinAges iterates over every holder's lot queue.
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

// AddCoins credits `amount` to an address as coin acquired at `acquiredAt`.
func (k Keeper) AddCoins(ctx sdk.Context, address string, amount math.Int, acquiredAt int64) {
	if !amount.IsPositive() {
		return
	}
	ca := k.GetCoinAge(ctx, address)
	ca.Address = address
	ca.Lots = types.InsertLot(ca.Lots, types.CoinLot{Amount: amount.String(), AcquiredAt: acquiredAt},
		k.GetParams(ctx).MaxCoinAgeLots)
	k.SetCoinAge(ctx, ca)
}

// SpendOldestFirst consumes `amount` from the front (oldest end) of an address's queue, persists the remainder, and returns the lots actually consumed — each carrying its own acquired_at.
func (k Keeper) SpendOldestFirst(ctx sdk.Context, address string, amount math.Int) []types.CoinLot {
	params := k.GetParams(ctx)
	ca := k.GetCoinAge(ctx, address)
	consumed, remaining := types.SpendOldestFirst(ca.Lots, amount, ctx.BlockTime().Unix(), params.CoinAgeThresholdSeconds)
	ca.Address = address
	ca.Lots = remaining
	k.SetCoinAge(ctx, ca)
	return consumed
}

// EarlyRedeemPenalty computes the tiered coin-age exit penalty for redeeming `redeemUphi` and consumes the holder's OLDEST coin first.
func (k Keeper) EarlyRedeemPenalty(ctx sdk.Context, address string, redeemUphi math.Int) math.Int {
	if !redeemUphi.IsPositive() {
		return math.ZeroInt()
	}
	consumed := k.SpendOldestFirst(ctx, address, redeemUphi)
	return types.PenaltyForLots(consumed, ctx.BlockTime().Unix(), k.GetParams(ctx))
}

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

// PruneMicroQuota deletes daily micro-exemption quota keys older than MicroQuotaRetentionDays under a fixed per-block budget, so a day-boundary rollover can never do O(keyset) deletes in a single block even if an adversary cheaply inflates one day's (day, address) keyset.
func (k Keeper) PruneMicroQuota(ctx sdk.Context) {
	cutoffDay := ctx.BlockTime().Unix()/86400 - types.MicroQuotaRetentionDays
	if cutoffDay <= 0 {
		return
	}
	store := ctx.KVStore(k.storeKey)
	it := store.Iterator(types.MicroQuotaPrefix, types.MicroQuotaDayBound(cutoffDay))
	defer it.Close()
	var stale [][]byte
	for n := 0; it.Valid() && n < types.MicroQuotaPruneBudget; it.Next() {
		stale = append(stale, append([]byte(nil), it.Key()...))
		n++
	}
	for _, key := range stale {
		store.Delete(key)
	}
}
