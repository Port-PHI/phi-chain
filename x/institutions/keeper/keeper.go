// SPDX-License-Identifier: Apache-2.0

package keeper

import (
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
	// verifier is the phi-crypto port: verifies institution deposit proofs (P-256) when a deposit key
	// is registered. Default build is fail-closed (rejects); real verification under -tags phicrypto_cgo.
	verifier phicrypto.Verifier
	// govKeeper is optional and injected post-construction (see SetGovKeeper): the gov keeper is
	// built after this keeper in app wiring. Used only by FinalizeFxEntry's passed-proposal gate.
	govKeeper types.GovKeeper
}

// NewKeeper creates a new keeper.
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

// GetAuthority returns the governance authority address.
func (k Keeper) GetAuthority() string { return k.authority }

// SetGovKeeper injects the gov keeper after construction (the gov keeper is built after this keeper
// in app wiring). Must be called before the institutions AppModule copies the keeper by value.
func (k *Keeper) SetGovKeeper(gk types.GovKeeper) { k.govKeeper = gk }

// Logger returns the module logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}

// --- Parameters ---

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
	// phi_to_toman is immutable while any vault holds balance. Rescaling the fixed rate with
	// non-zero vaults would instantly break solvency (supply×rate == Σvault×1e6) for state already
	// minted at the old rate. It may change only at genesis or while all vaults are empty (at the
	// genesis SetParams the institutions are not loaded yet, so Σvault is zero and this allows it).
	cur := k.GetParams(ctx)
	if cur.PhiToToman != p.PhiToToman && k.SumVaultBalance(ctx).IsPositive() {
		return errors.Wrapf(types.ErrInvalidParams,
			"phi_to_toman is immutable while vault balances are non-zero (current=%d, requested=%d)",
			cur.PhiToToman, p.PhiToToman)
	}
	ctx.KVStore(k.storeKey).Set(types.ParamsKey, k.cdc.MustMarshal(&p))
	return nil
}

// PenaltyDestination resolves where penalty coins (redirected slashed stake and governance deposit
// "burns") are routed so total uphi supply stays constant. It is the operator/governance account:
// the configured penalty_destination param, else the operator, else the governance authority.
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

// RedirectSlashedToPenalty keeps total supply constant across a validator slash. The app's
// slash-compensation wrapper measures the uphi the SDK burned across the whole slash (validator
// bonded burn plus unbonding-delegation/redelegation burns) and calls this to mint exactly that
// amount and route it to the penalty destination, so the net supply change is zero (solvency is
// preserved) and the slashed value accrues to the operator/governance account instead of leaving
// circulation.
func (k Keeper) RedirectSlashedToPenalty(ctx sdk.Context, slashedUphi math.Int) error {
	if !slashedUphi.IsPositive() {
		return nil
	}
	coins := cointypes.CoinsOf(slashedUphi)
	// Restore the burned supply FIRST so the global solvency invariant holds regardless of where the
	// coins finally land. A mint failure is a genuine consensus fault and still propagates.
	if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, coins); err != nil {
		return err
	}
	// Route to the penalty destination. A nil/blocked/unsendable destination must NOT halt the chain
	// from the slashing BeginBlocker: the minted coins stay escrowed in the module account
	// (supply is already restored, so solvency holds) and the shortfall is surfaced as an event for
	// governance to sweep. The param-set guard rejects a blocked dest up front; this is defense in depth.
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

// emitPenaltyEscrowed records that slashed-stake compensation was minted (supply restored) but could
// not be routed to the penalty destination, so it is held in the module account for governance to
// sweep.
func (k Keeper) emitPenaltyEscrowed(ctx sdk.Context, escrowedUphi math.Int, reason string) {
	k.Logger(ctx).Error("penalty compensation escrowed in module account", "uphi", escrowedUphi.String(), "reason", reason)
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypePenaltyEscrowed,
		sdk.NewAttribute(types.AttributeKeyEscrowedUphi, escrowedUphi.String()),
		sdk.NewAttribute(types.AttributeKeyReason, reason),
	))
}

// assertSolvency enforces the global solvency invariant on the keeper write path (not only via the
// periodic x/crisis sweep), so any mint/redeem that would desynchronize supply and vaults fails the
// transaction deterministically.
func (k Keeper) assertSolvency(ctx sdk.Context) error {
	if msg, broken := SolvencyInvariant(k)(ctx); broken {
		return errors.Wrap(types.ErrSolvencyBroken, msg)
	}
	return nil
}

// --- Institutions ---

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

func (k Keeper) DeleteInstitution(ctx sdk.Context, id string) {
	ctx.KVStore(k.storeKey).Delete(types.InstitutionKey(id))
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

// --- Solvency helpers ---

// TotalSupplyUphi returns the total uphi supply (from bank).
func (k Keeper) TotalSupplyUphi(ctx sdk.Context) math.Int {
	return k.bankKeeper.GetSupply(ctx, cointypes.Denom).Amount
}

// SumVaultBalance returns the sum of all institutions' vault balances (toman).
func (k Keeper) SumVaultBalance(ctx sdk.Context) math.Int {
	sum := math.ZeroInt()
	k.IterateInstitutions(ctx, func(inst types.Institution) bool {
		sum = sum.Add(mustInt(inst.VaultBalance))
		return false
	})
	return sum
}

// MintedUphiForToman converts a toman amount to uphi: uphi = toman * UphiPerPhi / phiToToman.
// The second return value reports whether the conversion is integral (no remainder).
func MintedUphiForToman(toman math.Int, phiToToman uint64) (math.Int, bool) {
	num := toman.Mul(math.NewIntFromUint64(cointypes.UphiPerPhi))
	den := math.NewIntFromUint64(phiToToman)
	q := num.Quo(den)
	r := num.Mod(den)
	return q, r.IsZero()
}

// mustInt converts a string to math.Int; invalid input yields zero.
func mustInt(s string) math.Int {
	if v, ok := math.NewIntFromString(s); ok {
		return v
	}
	return math.ZeroInt()
}
