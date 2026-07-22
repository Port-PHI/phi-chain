// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

func (f fixture) mintAs(signer, inst, ref string) error {
	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: signer, Institution: inst, Recipient: f.holder.String(),
		AmountToman: "100", DepositRef: ref,
	})
	return err
}

// A single-admin institution cannot mint at all.
func TestMintSeparation_SingleAdminInstitutionCannotMint(t *testing.T) {
	f := setup(t)
	f.registerAndAttestSoleAdmin(t, "bank-a", 100_000)

	require.Equal(t, uint32(2), types.MinAdminsForMint)
	require.ErrorIs(t, f.mintAs(f.admin.String(), "bank-a", "dep-1"), types.ErrTooFewAdmins,
		"one admin key means the approval threshold is a formality; minting must be refused")
}

// Granting a second distinct admin key unblocks it.
func TestMintSeparation_SecondAdminUnblocksMinting(t *testing.T) {
	f := setup(t)
	f.registerAndAttestSoleAdmin(t, "bank-a", 100_000)
	require.ErrorIs(t, f.mintAs(f.admin.String(), "bank-a", "dep-1"), types.ErrTooFewAdmins)

	f.k.SetRole(f.ctx, "bank-a", secondAdmin(), types.INSTITUTION_ROLE_ADMIN)

	require.NoError(t, f.mintAs(f.admin.String(), "bank-a", "dep-2"),
		"with two distinct admin keys and a separate attestor, the mint proceeds")
}

// The key that attested the reserve may not authorise the mint against it, even after gaining a minting role.
func TestMintSeparation_AttestorCannotMintAgainstItsOwnAttestation(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 100_000) // attested by the COMPLIANCE key

	f.k.SetRole(f.ctx, "bank-a", f.compliance, types.INSTITUTION_ROLE_ADMIN)

	require.ErrorIs(t, f.mintAs(f.compliance.String(), "bank-a", "dep-1"), types.ErrAttestorIsMinter,
		"the key that attested the reserve must not be the key that mints against it")

	require.NoError(t, f.mintAs(f.admin.String(), "bank-a", "dep-2"),
		"a distinct authoriser may mint against that attestation")
}

// Re-attesting moves the restriction: whoever attested LAST is the key barred from minting.
func TestMintSeparation_TheRestrictionFollowsTheLatestAttestor(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 100_000)

	otherCompliance := mkAddr("second_compliance")
	f.k.SetRole(f.ctx, "bank-a", otherCompliance, types.INSTITUTION_ROLE_COMPLIANCE)
	_, err := f.msg.PublishInstitutionAttestation(f.ctx, &types.MsgPublishInstitutionAttestation{
		Admin: otherCompliance.String(), Institution: "bank-a", AttestedReserve: "200000",
	})
	require.NoError(t, err)

	f.k.SetRole(f.ctx, "bank-a", otherCompliance, types.INSTITUTION_ROLE_ADMIN)
	require.ErrorIs(t, f.mintAs(otherCompliance.String(), "bank-a", "dep-1"), types.ErrAttestorIsMinter)

	f.k.SetRole(f.ctx, "bank-a", f.compliance, types.INSTITUTION_ROLE_ADMIN)
	require.NoError(t, f.mintAs(f.compliance.String(), "bank-a", "dep-2"),
		"only the key behind the CURRENT attestation is barred")
}

// An institution that has never attested cannot mint until it does.
func TestMintSeparation_NoRecordedAttestorRefusesTheMint(t *testing.T) {
	f := setup(t)
	_, err := f.msg.RegisterInstitution(f.ctx, &types.MsgRegisterInstitution{
		Operator: f.oper.String(), Id: "bank-a", License: "LIC-1", Admin: f.admin.String(),
		VaultAccount: "v", VaultApi: "x", Bond: "0", InstitutionType: types.INSTITUTION_TYPE_FINANCIAL,
	})
	require.NoError(t, err)
	f.k.SetRole(f.ctx, "bank-a", secondAdmin(), types.INSTITUTION_ROLE_ADMIN)

	_, found := f.k.LastAttestor(f.ctx, "bank-a")
	require.False(t, found, "no attestation has been published")
	require.ErrorIs(t, f.mintAs(f.admin.String(), "bank-a", "dep-1"), types.ErrAttestorIsMinter,
		"an institution with no recorded attestor must not mint")
}

// The attestor is recorded per institution.
func TestMintSeparation_AttestorIsRecordedPerInstitution(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 100_000)
	f.registerAndAttest(t, "bank-b", 100_000)

	a, foundA := f.k.LastAttestor(f.ctx, "bank-a")
	b, foundB := f.k.LastAttestor(f.ctx, "bank-b")
	require.True(t, foundA)
	require.True(t, foundB)
	require.Equal(t, f.compliance.Bytes(), a.Bytes())
	require.Equal(t, f.compliance.Bytes(), b.Bytes())

	f.k.SetRole(f.ctx, "bank-a", f.compliance, types.INSTITUTION_ROLE_ADMIN)
	require.ErrorIs(t, f.mintAs(f.compliance.String(), "bank-a", "dep-a"), types.ErrAttestorIsMinter)
	require.NoError(t, f.mintAs(f.admin.String(), "bank-b", "dep-b"))
}

// Redemption is never gated by any of this: a holder can still exit an institution that cannot mint.
func TestMintSeparation_RedemptionIsNeverGated(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 100_000)
	require.NoError(t, f.mintAs(f.admin.String(), "bank-a", "dep-1"))

	f.k.DeleteRole(f.ctx, "bank-a", secondAdmin())
	require.ErrorIs(t, f.mintAs(f.admin.String(), "bank-a", "dep-2"), types.ErrTooFewAdmins)

	_, err := f.msg.InstitutionRedeem(f.ctx, &types.MsgInstitutionRedeem{
		Admin: f.holder.String(), Institution: "bank-a", Holder: f.holder.String(),
		AmountToman: "100", RedeemRef: "red-1",
	})
	require.NoError(t, err, "redemption must never be gated by the mint-side separation rules")
}
