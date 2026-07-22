// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"fmt"

	"cosmossdk.io/errors"
	"cosmossdk.io/log"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/phicrypto"
	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

// Keeper manages the institution registry state.
type Keeper struct {
	cdc            codec.BinaryCodec
	storeKey       storetypes.StoreKey
	authority      string
	bankKeeper     types.BankKeeper
	identityKeeper types.IdentityKeeper
	coinKeeper     types.CoinKeeper
	// verifier is the phi-crypto port; fail-closed by default, real verification under -tags phicrypto_cgo.
	verifier phicrypto.Verifier
	// govKeeper is injected post-construction (SetGovKeeper); used only by FinalizeFxEntry's passed-proposal gate.
	govKeeper types.GovKeeper
}

func NewKeeper(
	cdc codec.BinaryCodec,
	storeKey storetypes.StoreKey,
	authority string,
	bank types.BankKeeper,
	identity types.IdentityKeeper,
	coin types.CoinKeeper,
	verifier phicrypto.Verifier,
) Keeper {
	return Keeper{
		cdc:            cdc,
		storeKey:       storeKey,
		authority:      authority,
		bankKeeper:     bank,
		identityKeeper: identity,
		coinKeeper:     coin,
		verifier:       verifier,
	}
}

func (k Keeper) GetAuthority() string { return k.authority }

// SetGovKeeper injects the gov keeper after construction; call before the AppModule copies the keeper by value.
func (k *Keeper) SetGovKeeper(gk types.GovKeeper) { k.govKeeper = gk }

func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}

func (k Keeper) GetParams(ctx sdk.Context) (p types.Params) {
	bz := ctx.KVStore(k.storeKey).Get(types.ParamsKey)
	if bz == nil {
		return types.DefaultParams()
	}
	k.cdc.MustUnmarshal(bz, &p)
	return p
}

func (k Keeper) SetParams(ctx sdk.Context, p types.Params) error {
	if err := p.Validate(); err != nil {
		return err
	}
	// phi_to_toman is immutable while any vault holds balance: rescaling would break solvency (supply×rate == Σvault×1e6).
	cur := k.GetParams(ctx)
	if cur.PhiToToman != p.PhiToToman && k.SumVaultBalance(ctx).IsPositive() {
		return errors.Wrapf(types.ErrInvalidParams,
			"phi_to_toman is immutable while vault balances are non-zero (current=%d, requested=%d)",
			cur.PhiToToman, p.PhiToToman)
	}
	ctx.KVStore(k.storeKey).Set(types.ParamsKey, k.cdc.MustMarshal(&p))
	return nil
}

// PenaltyDestination routes penalty coins to penalty_destination param, else operator, else authority.
func (k Keeper) PenaltyDestination(ctx sdk.Context) sdk.AccAddress {
	p := k.GetParams(ctx)
	for _, candidate := range []string{p.PenaltyDestination, p.Operator, k.authority} {
		if candidate == "" {
			continue
		}
		if addr, err := sdk.AccAddressFromBech32(candidate); err == nil {
			return addr
		}
	}
	return nil
}

// RedirectSlashedToPenalty mints exactly the slashed uphi and routes it to the penalty destination, keeping supply constant.
func (k Keeper) RedirectSlashedToPenalty(ctx sdk.Context, slashedUphi math.Int) error {
	if !slashedUphi.IsPositive() {
		return nil
	}
	coins := cointypes.CoinsOf(slashedUphi)
	// Restore burned supply FIRST so solvency holds regardless of routing; a mint failure is a consensus fault.
	if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, coins); err != nil {
		return err
	}
	// An unsendable destination must not halt the chain: coins stay escrowed (supply already restored) and an event is emitted.
	dest := k.PenaltyDestination(ctx)
	if dest == nil {
		k.emitPenaltyEscrowed(ctx, slashedUphi, "no penalty destination resolved")
		return nil
	}
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, dest, coins); err != nil {
		k.emitPenaltyEscrowed(ctx, slashedUphi, err.Error())
		return nil
	}
	return nil
}

func (k Keeper) emitPenaltyEscrowed(ctx sdk.Context, escrowedUphi math.Int, reason string) {
	k.Logger(ctx).Error("penalty compensation escrowed in module account", "uphi", escrowedUphi.String(), "reason", reason)
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypePenaltyEscrowed,
		sdk.NewAttribute(types.AttributeKeyEscrowedUphi, escrowedUphi.String()),
		sdk.NewAttribute(types.AttributeKeyReason, reason),
	))
}

func (k Keeper) assertSolvency(ctx sdk.Context) error {
	if msg, broken := SolvencyInvariant(k)(ctx); broken {
		return errors.Wrap(types.ErrSolvencyBroken, msg)
	}
	return nil
}

func (k Keeper) SetInstitution(ctx sdk.Context, inst types.Institution) {
	ctx.KVStore(k.storeKey).Set(types.InstitutionKey(inst.Id), k.cdc.MustMarshal(&inst))
}

func (k Keeper) GetInstitution(ctx sdk.Context, id string) (types.Institution, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.InstitutionKey(id))
	if bz == nil {
		return types.Institution{}, false
	}
	var inst types.Institution
	k.cdc.MustUnmarshal(bz, &inst)
	return inst, true
}

func (k Keeper) HasInstitution(ctx sdk.Context, id string) bool {
	return ctx.KVStore(k.storeKey).Has(types.InstitutionKey(id))
}

func (k Keeper) removeInstitutionSingleRecords(ctx sdk.Context, id string) {
	store := ctx.KVStore(k.storeKey)
	store.Delete(types.InstitutionKey(id))
	store.Delete(types.AdminEpochKey(id))
	store.Delete(types.LastAttestorKey(id))
}

// IterateInstitutions iterates over all institutions; returning true stops the loop.
func (k Keeper) IterateInstitutions(ctx sdk.Context, cb func(types.Institution) bool) {
	store := ctx.KVStore(k.storeKey)
	it := storetypes.KVStorePrefixIterator(store, types.InstitutionPrefix)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var inst types.Institution
		k.cdc.MustUnmarshal(it.Value(), &inst)
		if cb(inst) {
			break
		}
	}
}

func (k Keeper) TotalSupplyUphi(ctx sdk.Context) math.Int {
	return k.bankKeeper.GetSupply(ctx, cointypes.Denom).Amount
}

// SumVaultBalance sums all vault balances (toman) FAIL-CLOSED: an unparseable value panics rather than masking a shortfall.
func (k Keeper) SumVaultBalance(ctx sdk.Context) math.Int {
	sum := math.ZeroInt()
	k.IterateInstitutions(ctx, func(inst types.Institution) bool {
		sum = sum.Add(mustIntStrict(inst.VaultBalance, inst.Id))
		return false
	})
	return sum
}

// MintedUphiForToman converts toman to uphi; second return reports whether the conversion is integral.
func MintedUphiForToman(toman math.Int, phiToToman uint64) (math.Int, bool) {
	num := toman.Mul(math.NewIntFromUint64(cointypes.UphiPerPhi))
	den := math.NewIntFromUint64(phiToToman)
	q := num.Quo(den)
	r := num.Mod(den)
	return q, r.IsZero()
}

func mustInt(s string) math.Int {
	if v, ok := math.NewIntFromString(s); ok {
		return v
	}
	return math.ZeroInt()
}

func mustIntStrict(s, instID string) math.Int {
	v, ok := math.NewIntFromString(s)
	if !ok {
		panic(fmt.Sprintf("institution %q: unparseable vault_balance %q (solvency read)", instID, s))
	}
	return v
}
