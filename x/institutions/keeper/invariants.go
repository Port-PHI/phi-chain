// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"fmt"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

// RegisterInvariants registers the consensus-halting solvency invariants with x/crisis.
//
// Only invariants that a *legitimate* state transition can never falsify are registered, since a
// broken registered invariant halts the chain (and is reachable permissionlessly via
// MsgVerifyInvariant). "mint-within-backing" is deliberately NOT registered: an honest
// attestation of a reduced reserve (LOW_LIQ) is an allowed state that would falsify it, so it is a
// non-halting health signal (see EmitBackingHealth / BackingShortfallInvariant) instead.
//
// Each registered invariant is enforced in all three places: the keeper on write (assertSolvency),
// here as a registered invariant, and in tests.
func RegisterInvariants(ir sdk.InvariantRegistry, k Keeper) {
	ir.RegisterRoute(types.ModuleName, "solvency", SolvencyInvariant(k))
	ir.RegisterRoute(types.ModuleName, "non-negative-vault", NonNegativeVaultInvariant(k))
}

// AllInvariants checks every registered (halting) invariant in a single pass. It intentionally
// excludes mint-within-backing so an allowed LOW_LIQ state never reports as broken.
func AllInvariants(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		for _, inv := range []sdk.Invariant{
			SolvencyInvariant(k),
			NonNegativeVaultInvariant(k),
		} {
			if msg, broken := inv(ctx); broken {
				return msg, broken
			}
		}
		return "", false
	}
}

// SolvencyInvariant is the global solvency invariant (multi-institution model):
//
//	TotalSupply(phi) * phi_to_toman = sum(vault_balance(toman))
//
// Since supply is denominated in uphi, it is written as a cross-multiplication to avoid fractions:
//
//	supply_uphi * phi_to_toman == sum(vault_balance) * UphiPerPhi
//
// (UphiPerPhi = 1,000,000; with phi_to_toman = 100,000 this equals supply_uphi = 10 * sum(vault_balance).)
func SolvencyInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		phiToToman := math.NewIntFromUint64(k.GetParams(ctx).PhiToToman)
		uphiPerPhi := math.NewIntFromUint64(cointypes.UphiPerPhi)

		supply := k.TotalSupplyUphi(ctx)
		sumVault := k.SumVaultBalance(ctx)

		lhs := supply.Mul(phiToToman)
		rhs := sumVault.Mul(uphiPerPhi)
		broken := !lhs.Equal(rhs)
		return sdk.FormatInvariant(types.ModuleName, "solvency",
			fmt.Sprintf("supply_uphi×phi_to_toman=%s but Σvault_balance×UphiPerPhi=%s", lhs, rhs)), broken
	}
}

// NonNegativeVaultInvariant: no institution pays out more than its vault -> vault_balance >= 0.
func NonNegativeVaultInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		broken := false
		var bad string
		k.IterateInstitutions(ctx, func(inst types.Institution) bool {
			if mustInt(inst.VaultBalance).IsNegative() {
				broken, bad = true, inst.Id
				return true
			}
			return false
		})
		return sdk.FormatInvariant(types.ModuleName, "non-negative-vault",
			fmt.Sprintf("institution %q has negative vault_balance", bad)), broken
	}
}

// BackingShortfallInvariant: each institution's minted phi (vault_balance) <= its own attested
// reserve. This is a NON-registered health check: an honest attestation of a reduced reserve is
// an allowed LOW_LIQ state, so a shortfall must not halt consensus. It is exposed in invariant form
// for off-chain monitors and tests; the on-chain signal is EmitBackingHealth.
func BackingShortfallInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		broken := false
		var bad string
		k.IterateInstitutions(ctx, func(inst types.Institution) bool {
			if mustInt(inst.VaultBalance).GT(mustInt(inst.AttestedReserve)) {
				broken, bad = true, inst.Id
				return true
			}
			return false
		})
		return sdk.FormatInvariant(types.ModuleName, "backing-shortfall",
			fmt.Sprintf("institution %q vault_balance exceeds attested_reserve", bad)), broken
	}
}

// EmitBackingHealth reports an institution's backing health: when its vault exceeds its attested
// reserve it sets a per-institution telemetry gauge of the shortfall and emits a backing-shortfall
// event. This is the non-halting replacement for the old mint-within-backing invariant — LOW_LIQ is
// an allowed state, surfaced for monitoring (phi-bridge) rather than halting the chain.
func (k Keeper) EmitBackingHealth(ctx sdk.Context, inst types.Institution) {
	shortfall := mustInt(inst.VaultBalance).Sub(mustInt(inst.AttestedReserve))
	if !shortfall.IsPositive() {
		return
	}
	// Int64() panics out of range; saturate the gauge for an (implausibly) huge shortfall. The exact
	// value is always carried losslessly by the event below.
	gauge := float32(3.4e38)
	if shortfall.IsInt64() {
		gauge = float32(shortfall.Int64())
	}
	telemetry.SetGauge(gauge, types.ModuleName, "backing_shortfall_toman")
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeBackingShortfall,
		sdk.NewAttribute(types.AttributeKeyInstitution, inst.Id),
		sdk.NewAttribute(types.AttributeKeyAttestedReserve, inst.AttestedReserve),
		sdk.NewAttribute(types.AttributeKeyAmountToman, inst.VaultBalance),
		sdk.NewAttribute(types.AttributeKeyShortfallToman, shortfall.String()),
	))
}
