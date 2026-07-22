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

func (f fixture) mintBacked(t *testing.T, id string, toman int64, ref string) {
	t.Helper()
	f.registerAndAttest(t, id, toman)
	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: id, Recipient: f.holder.String(),
		AmountToman: math.NewInt(toman).String(), DepositRef: ref,
	})
	require.NoError(t, err)
}

// A slash burns bond-pool stake and the escrow re-mints it to the penalty destination, so supply (and solvency) is unchanged.
func TestSlashRedirect_KeepsSupplyConstantAndCreditsPenaltyDest(t *testing.T) {
	f := setup(t)
	f.mintBacked(t, "bank-a", 1000, "dep-1") // supply 10,000 uphi backed by 1000 toman

	supplyBefore := f.bank.GetSupply(f.ctx, cointypes.Denom).Amount
	slashed := math.NewInt(5000)

	require.NoError(t, f.bank.BurnCoins(f.ctx, "bonded_tokens_pool", cointypes.CoinsOf(slashed)))
	require.NoError(t, f.k.RedirectSlashedToPenalty(f.ctx, slashed))

	require.Equal(t, supplyBefore, f.bank.GetSupply(f.ctx, cointypes.Denom).Amount)
	_, broken := keeper.SolvencyInvariant(f.k)(f.ctx)
	require.False(t, broken, "supply is unchanged, so solvency must hold across a slash")

	require.Equal(t, slashed, f.bank.GetBalance(f.ctx, f.oper, cointypes.Denom).Amount)
}

// The penalty destination resolves to the param, else the operator, else the governance authority.
func TestPenaltyDestination_Resolution(t *testing.T) {
	f := setup(t)
	require.Equal(t, f.oper, f.k.PenaltyDestination(f.ctx))

	dest := sdk.AccAddress([]byte("penalty_destination_"))
	p := f.k.GetParams(f.ctx)
	p.PenaltyDestination = dest.String()
	require.NoError(t, f.k.SetParams(f.ctx, p))
	require.Equal(t, dest, f.k.PenaltyDestination(f.ctx))

	p.PenaltyDestination, p.Operator = "", ""
	require.NoError(t, f.k.SetParams(f.ctx, p))
	authAddr, err := sdk.AccAddressFromBech32(f.authority)
	require.NoError(t, err)
	require.Equal(t, authAddr, f.k.PenaltyDestination(f.ctx))
}

// A LOW_LIQ attestation (reserve below vault) is allowed: it must not break the halting invariants, only the health check.
func TestAttestationBelowVault_IsHealthSignalNotHalt(t *testing.T) {
	f := setup(t)
	f.mintBacked(t, "bank-a", 1000, "dep-1")

	_, err := f.msg.PublishInstitutionAttestation(f.ctx, &types.MsgPublishInstitutionAttestation{
		Admin: f.compliance.String(), Institution: "bank-a", AttestedReserve: "600", // below the 1000 vault
	})
	require.NoError(t, err)

	inst, _ := f.k.GetInstitution(f.ctx, "bank-a")
	require.Equal(t, types.INSTITUTION_STATUS_LOW_LIQ, inst.Status)

	_, broken := keeper.AllInvariants(f.k)(f.ctx)
	require.False(t, broken, "a legitimate LOW_LIQ attestation must not break the halting invariants")
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

// phi_to_toman is immutable while any vault holds balance (rescaling would break solvency for minted coins).
func TestPhiToToman_ImmutableWhileVaultsNonEmpty(t *testing.T) {
	f := setup(t)

	p := f.k.GetParams(f.ctx)
	p.PhiToToman = 200_000
	require.NoError(t, f.k.SetParams(f.ctx, p))
	p.PhiToToman = 100_000
	require.NoError(t, f.k.SetParams(f.ctx, p))

	f.mintBacked(t, "bank-a", 1000, "dep-1")
	p = f.k.GetParams(f.ctx)
	p.PhiToToman = 200_000
	require.ErrorIs(t, f.k.SetParams(f.ctx, p), types.ErrInvalidParams)

	p.PhiToToman = 100_000
	require.NoError(t, f.k.SetParams(f.ctx, p))
}

// ExportGenesis carries anti-replay markers, cap counters, and approvals; re-import restores them exactly.
func TestGenesisRoundTrip_MarkersCapsApprovals(t *testing.T) {
	f := setup(t)
	f.mintBacked(t, "bank-a", 1000, "dep-1") // writes a deposit marker + cap counters

	contentHash := make([]byte, 32)
	f.ctx.KVStore(f.key).Set(types.ApprovalKey("bank-a", contentHash, f.oper), make([]byte, 8))

	exported := f.k.ExportGenesis(f.ctx)
	require.NoError(t, exported.Validate())
	require.NotEmpty(t, exported.DepositMarkers, "deposit markers must be exported")
	require.NotEmpty(t, exported.CapCounters, "cap counters must be exported")
	require.NotEmpty(t, exported.Approvals, "approvals must be exported")

	f2 := setup(t)
	f2.bank.supply[cointypes.Denom] = math.NewInt(10000)
	f2.k.InitGenesis(f2.ctx, *exported)

	_, err := f2.msg.InstitutionMint(f2.ctx, &types.MsgInstitutionMint{
		Admin: f2.admin.String(), Institution: "bank-a", Recipient: f2.holder.String(),
		AmountToman: "1000", DepositRef: "dep-1",
	})
	require.ErrorIs(t, err, types.ErrDuplicateDeposit)

	require.True(t, f2.ctx.KVStore(f2.key).Has(types.ApprovalKey("bank-a", contentHash, f2.oper)))
}

// A slash keeps supply constant, so an exported genesis still boots (solvency holds after a slash).
func TestGenesisRoundTrip_BootsAfterSlash(t *testing.T) {
	f := setup(t)
	f.mintBacked(t, "bank-a", 1000, "dep-1") // supply 10,000 uphi, vault 1000 toman

	slashed := math.NewInt(3000)
	require.NoError(t, f.bank.BurnCoins(f.ctx, "bonded_tokens_pool", cointypes.CoinsOf(slashed)))
	require.NoError(t, f.k.RedirectSlashedToPenalty(f.ctx, slashed))

	exported := f.k.ExportGenesis(f.ctx)

	f2 := setup(t)
	f2.bank.supply[cointypes.Denom] = f.bank.GetSupply(f.ctx, cointypes.Denom).Amount
	require.NotPanics(t, func() { f2.k.InitGenesis(f2.ctx, *exported) })
}
