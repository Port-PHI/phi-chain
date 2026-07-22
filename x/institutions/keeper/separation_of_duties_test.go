// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	"github.com/Port-PHI/phi-chain/x/institutions/keeper"
	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

// TestSeparationOfDuties_AttestNeMint is the separation-of-duties matrix: no single institution key can both raise the attested reserve AND mint against it, because the attest role set {COMPLIANCE} is disjoint from the mint role set {OPERATOR, ADMIN} and every address has exactly one effective role.
func TestSeparationOfDuties_AttestNeMint(t *testing.T) {
	f := setup(t)
	_, err := f.msg.RegisterInstitution(f.ctx, &types.MsgRegisterInstitution{
		Operator: f.oper.String(), Id: "bank-a", License: "LIC-1", Admin: f.admin.String(),
		VaultAccount: "v", VaultApi: "x", Bond: "0", InstitutionType: types.INSTITUTION_TYPE_FINANCIAL,
	})
	require.NoError(t, err)

	comp := mkAddr("compliance_key")
	f.k.SetRole(f.ctx, "bank-a", comp, types.INSTITUTION_ROLE_COMPLIANCE)
	f.k.SetRole(f.ctx, "bank-a", secondAdmin(), types.INSTITUTION_ROLE_ADMIN)
	f.setSensitiveThreshold(t, "bank-a", 1)

	attest := func(signer string, reserve string) error {
		_, err := f.msg.PublishInstitutionAttestation(f.ctx, &types.MsgPublishInstitutionAttestation{
			Admin: signer, Institution: "bank-a", AttestedReserve: reserve,
		})
		return err
	}
	mint := func(signer, ref string) error {
		_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
			Admin: signer, Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "1000", DepositRef: ref,
		})
		return err
	}

	require.ErrorIs(t, attest(f.admin.String(), "1000"), types.ErrRoleNotAuthorized, "the admin must not attest")

	f.k.SetRole(f.ctx, "bank-a", f.admin, types.INSTITUTION_ROLE_COMPLIANCE)
	require.ErrorIs(t, attest(f.admin.String(), "1000"), types.ErrRoleNotAuthorized, "the admin cannot buy the attest right with a COMPLIANCE grant")

	op := mkAddr("operator_key")
	f.k.SetRole(f.ctx, "bank-a", op, types.INSTITUTION_ROLE_OPERATOR)
	require.ErrorIs(t, attest(op.String(), "1000"), types.ErrRoleNotAuthorized, "an operator must not attest")

	require.NoError(t, attest(comp.String(), "1000"), "the compliance officer attests")
	require.ErrorIs(t, mint(comp.String(), "by-compliance"), types.ErrRoleNotAuthorized, "the attester must not be able to mint")

	f.k.SetRole(f.ctx, "bank-a", comp, types.INSTITUTION_ROLE_OPERATOR)
	require.ErrorIs(t, attest(comp.String(), "1000"), types.ErrRoleNotAuthorized, "after re-grant the address is OPERATOR-only and cannot attest")

	supplyBefore := f.bank.GetSupply(f.ctx, cointypes.Denom).Amount
	require.NoError(t, mint(f.admin.String(), "by-admin"), "the admin mints against the compliance-attested reserve")

	inst, _ := f.k.GetInstitution(f.ctx, "bank-a")
	require.Equal(t, "1000", inst.VaultBalance, "vault rises by the minted toman")
	supplyAfter := f.bank.GetSupply(f.ctx, cointypes.Denom).Amount
	require.Equal(t, "10000", supplyAfter.Sub(supplyBefore).String(), "supply rises by 1000 toman × 10 uphi/toman")

	_, broken := keeper.SolvencyInvariant(f.k)(f.ctx)
	require.False(t, broken, "solvency invariant must hold after the separated mint")
	_, broken = keeper.NonNegativeVaultInvariant(f.k)(f.ctx)
	require.False(t, broken, "vault must be non-negative")
}
