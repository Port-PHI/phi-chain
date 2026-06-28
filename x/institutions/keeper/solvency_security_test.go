// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	"github.com/Port-PHI/phi-chain/x/institutions/keeper"
	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

// mintBacked registers an institution with full backing and mints `toman`, leaving supply backed by
// the vault (the solvency invariant holds).
func (f fixture) mintBacked(t *testing.T, id string, toman int64, ref string) {
	t.Helper()
	f.registerAndAttest(t, id, toman)
	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: id, Recipient: f.holder.String(),
		AmountToman: math.NewInt(toman).String(), DepositRef: ref,
	})
	require.NoError(t, err)
}

// A slash burns stake from the bond pool; the escrow mints the same amount to the penalty
// destination, so total supply (and therefore solvency) is unchanged and the value is not destroyed.
func TestSlashRedirect_KeepsSupplyConstantAndCreditsPenaltyDest(t *testing.T) {
	f := setup(t)
	f.mintBacked(t, "bank-a", 1000, "dep-1") // supply 10,000 uphi backed by 1000 toman

	supplyBefore := f.bank.GetSupply(f.ctx, cointypes.Denom).Amount
	slashed := math.NewInt(5000)

	// Simulate staking's slash: the bond pool burns `slashed`, then the escrow mints it back.
	require.NoError(t, f.bank.BurnCoins(f.ctx, "bonded_tokens_pool", cointypes.CoinsOf(slashed)))
	require.NoError(t, f.k.RedirectSlashedToPenalty(f.ctx, slashed))

	// Net supply change is zero → the solvency invariant still holds.
	require.Equal(t, supplyBefore, f.bank.GetSupply(f.ctx, cointypes.Denom).Amount)
	_, broken := keeper.SolvencyInvariant(f.k)(f.ctx)
	require.False(t, broken, "supply is unchanged, so solvency must hold across a slash")

	// The slashed value accrued to the penalty destination (the operator account by default).
	require.Equal(t, slashed, f.bank.GetBalance(f.ctx, f.oper, cointypes.Denom).Amount)
}

// The penalty destination resolves to the param, else the operator, else the governance authority.
func TestPenaltyDestination_Resolution(t *testing.T) {
	f := setup(t)
	// Default setup sets Operator → resolves to the operator.
	require.Equal(t, f.oper, f.k.PenaltyDestination(f.ctx))

	// An explicit penalty_destination param takes precedence.
	dest := sdk.AccAddress([]byte("penalty_destination_"))
	p := f.k.GetParams(f.ctx)
	p.PenaltyDestination = dest.String()
	require.NoError(t, f.k.SetParams(f.ctx, p))
	require.Equal(t, dest, f.k.PenaltyDestination(f.ctx))

	// With neither set, it falls back to the governance authority account.
	p.PenaltyDestination, p.Operator = "", ""
	require.NoError(t, f.k.SetParams(f.ctx, p))
	authAddr, err := sdk.AccAddressFromBech32(f.authority)
	require.NoError(t, err)
	require.Equal(t, authAddr, f.k.PenaltyDestination(f.ctx))
}

// An honest attestation of a reserve below the minted vault (LOW_LIQ) is an allowed state. It
// must NOT break the registered (halting) invariants — only the non-registered health check — and it
// emits a backing-shortfall event.
func TestAttestationBelowVault_IsHealthSignalNotHalt(t *testing.T) {
	f := setup(t)
	f.mintBacked(t, "bank-a", 1000, "dep-1")

	_, err := f.msg.PublishInstitutionAttestation(f.ctx, &types.MsgPublishInstitutionAttestation{
		Admin: f.admin.String(), Institution: "bank-a", AttestedReserve: "600", // below the 1000 vault
	})
	require.NoError(t, err)

	inst, _ := f.k.GetInstitution(f.ctx, "bank-a")
	require.Equal(t, types.INSTITUTION_STATUS_LOW_LIQ, inst.Status)

	// The registered invariants (solvency + non-negative-vault) are unaffected: supply and vault are
	// untouched by an attestation, so a permissionless MsgVerifyInvariant cannot halt the chain.
	_, broken := keeper.AllInvariants(f.k)(f.ctx)
	require.False(t, broken, "a legitimate LOW_LIQ attestation must not break the halting invariants")
	// The non-halting health check does detect the shortfall.
	_, short := keeper.BackingShortfallInvariant(f.k)(f.ctx)
	require.True(t, short)

	found := false
	for _, e := range f.ctx.EventManager().Events() {
		if e.Type == types.EventTypeBackingShortfall {
			found = true
		}
	}
	require.True(t, found, "a backing-shortfall health event must be emitted")
}

// The fixed rate phi_to_toman is immutable while any vault holds balance (rescaling it would
// instantly break solvency for already-minted coins); it may change only while all vaults are empty.
func TestPhiToToman_ImmutableWhileVaultsNonEmpty(t *testing.T) {
	f := setup(t)

	// With no vaults yet, the rate may be changed.
	p := f.k.GetParams(f.ctx)
	p.PhiToToman = 200_000
	require.NoError(t, f.k.SetParams(f.ctx, p))
	p.PhiToToman = 100_000
	require.NoError(t, f.k.SetParams(f.ctx, p))

	// After a backed mint (non-zero vault), changing the rate is rejected.
	f.mintBacked(t, "bank-a", 1000, "dep-1")
	p = f.k.GetParams(f.ctx)
	p.PhiToToman = 200_000
	require.ErrorIs(t, f.k.SetParams(f.ctx, p), types.ErrInvalidParams)

	// Setting the same rate is always allowed (no change).
	p.PhiToToman = 100_000
	require.NoError(t, f.k.SetParams(f.ctx, p))
}

// ExportGenesis carries the deposit/redeem anti-replay markers, the cap counters, and the
// accumulated approvals; re-importing restores them exactly (no replay window after an upgrade).
func TestGenesisRoundTrip_MarkersCapsApprovals(t *testing.T) {
	f := setup(t)
	f.mintBacked(t, "bank-a", 1000, "dep-1") // writes a deposit marker + cap counters

	// Seed an approval marker (these are written by the multisig sensitive-action flow).
	contentHash := make([]byte, 32)
	f.ctx.KVStore(f.key).Set(types.ApprovalKey("bank-a", contentHash, f.oper), []byte{1})

	exported := f.k.ExportGenesis(f.ctx)
	require.NoError(t, exported.Validate())
	require.NotEmpty(t, exported.DepositMarkers, "deposit markers must be exported")
	require.NotEmpty(t, exported.CapCounters, "cap counters must be exported")
	require.NotEmpty(t, exported.Approvals, "approvals must be exported")

	// Re-import into a fresh keeper. Seed the matching bank supply (in the real chain the bank module
	// genesis restores it) so the genesis solvency check passes.
	f2 := setup(t)
	f2.bank.supply[cointypes.Denom] = math.NewInt(10000)
	f2.k.InitGenesis(f2.ctx, *exported)

	// The deposit marker survived: re-minting the same ref is rejected as a duplicate (no replay).
	_, err := f2.msg.InstitutionMint(f2.ctx, &types.MsgInstitutionMint{
		Admin: f2.admin.String(), Institution: "bank-a", Recipient: f2.holder.String(),
		AmountToman: "1000", DepositRef: "dep-1",
	})
	require.ErrorIs(t, err, types.ErrDuplicateDeposit)

	// The approval marker survived verbatim.
	require.True(t, f2.ctx.KVStore(f2.key).Has(types.ApprovalKey("bank-a", contentHash, f2.oper)))
}

// A slash keeps supply constant, so an exported genesis still satisfies the solvency invariant
// and re-imports without panicking — the chain boots after a slash.
func TestGenesisRoundTrip_BootsAfterSlash(t *testing.T) {
	f := setup(t)
	f.mintBacked(t, "bank-a", 1000, "dep-1") // supply 10,000 uphi, vault 1000 toman

	// Slash: the bond pool burns, the escrow mints the same amount back to the penalty destination.
	slashed := math.NewInt(3000)
	require.NoError(t, f.bank.BurnCoins(f.ctx, "bonded_tokens_pool", cointypes.CoinsOf(slashed)))
	require.NoError(t, f.k.RedirectSlashedToPenalty(f.ctx, slashed))

	exported := f.k.ExportGenesis(f.ctx)

	// Re-import into a fresh keeper; seed the matching bank supply (the bank module genesis restores
	// it in the real chain). InitGenesis runs the solvency invariant and panics if it is broken.
	f2 := setup(t)
	f2.bank.supply[cointypes.Denom] = f.bank.GetSupply(f.ctx, cointypes.Denom).Amount
	require.NotPanics(t, func() { f2.k.InitGenesis(f2.ctx, *exported) })
}
