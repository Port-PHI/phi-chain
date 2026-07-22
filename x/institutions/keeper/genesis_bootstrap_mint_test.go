// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

func bootstrapGenesis(t *testing.T, rootAdmin, secondAdmin sdk.AccAddress) types.GenesisState {
	t.Helper()
	gs := types.DefaultGenesis()
	gs.Institutions = []types.Institution{{
		Id: "boot-bank", License: "lic", Admin: rootAdmin.String(),
		VaultAccount: "vault", VaultApi: "https://vault.example",
		Bond: "0", Status: types.INSTITUTION_STATUS_HEALTHY,
		VaultBalance: "0", AttestedReserve: "1000000",
		InstitutionType: types.INSTITUTION_TYPE_FINANCIAL,
	}}
	gs.RoleGrants = []types.RoleGrant{
		{Institution: "boot-bank", Address: secondAdmin.String(), Role: types.INSTITUTION_ROLE_ADMIN},
	}
	return *gs
}

// TestGenesisBootstrap_AGenesisInstitutionCanMintAtBlockOne is the bootstrap the design intends.
func TestGenesisBootstrap_AGenesisInstitutionCanMintAtBlockOne(t *testing.T) {
	f := setup(t)
	root := sdk.AccAddress([]byte("boot-root-admin_____"))
	second := sdk.AccAddress([]byte("boot-second-admin___"))

	f.k.InitGenesis(f.ctx, bootstrapGenesis(t, root, second))

	attestor, found := f.k.LastAttestor(f.ctx, "boot-bank")
	require.True(t, found, "a genesis attested_reserve is an attestation and must have an attestor")
	require.Equal(t, root, attestor)

	require.NoError(t, f.mintAs(second.String(), "boot-bank", "boot-dep-1"),
		"a properly configured genesis institution must be able to mint at block 1")
}

// The separation still binds.
func TestGenesisBootstrap_TheAttributedAttestorStillCannotMint(t *testing.T) {
	f := setup(t)
	root := sdk.AccAddress([]byte("boot-root-admin_____"))
	second := sdk.AccAddress([]byte("boot-second-admin___"))

	f.k.InitGenesis(f.ctx, bootstrapGenesis(t, root, second))

	require.ErrorIs(t, f.mintAs(root.String(), "boot-bank", "boot-dep-x"), types.ErrAttestorIsMinter,
		"the key credited with the genesis attestation must not also authorise the mint")
}

// And the two-distinct-admins rule still binds: a genesis institution with only its root admin cannot mint at all, because there is no second key to sign.
func TestGenesisBootstrap_ASingleAdminGenesisInstitutionStillCannotMint(t *testing.T) {
	f := setup(t)
	root := sdk.AccAddress([]byte("boot-root-admin_____"))

	gs := bootstrapGenesis(t, root, sdk.AccAddress([]byte("boot-second-admin___")))
	gs.RoleGrants = nil // the root admin alone
	f.k.InitGenesis(f.ctx, gs)

	require.ErrorIs(t, f.mintAs(root.String(), "boot-bank", "boot-dep-y"), types.ErrTooFewAdmins,
		"one admin key is one key, whether it arrived by genesis or by registration")
}

// A genesis that names its own attestor wins over the default, which is also what keeps an export→import exact: the exported attestor is restored verbatim.
func TestGenesisBootstrap_AnExplicitAttestorOverridesTheDefault(t *testing.T) {
	f := setup(t)
	root := sdk.AccAddress([]byte("boot-root-admin_____"))
	second := sdk.AccAddress([]byte("boot-second-admin___"))
	stated := sdk.AccAddress([]byte("boot-stated-attestor"))

	gs := bootstrapGenesis(t, root, second)
	gs.StoreEntries = []types.StoreEntry{
		{Key: types.LastAttestorKey("boot-bank"), Value: stated.Bytes()},
	}
	f.k.InitGenesis(f.ctx, gs)

	attestor, found := f.k.LastAttestor(f.ctx, "boot-bank")
	require.True(t, found)
	require.Equal(t, stated, attestor, "a stated attestor must not be overwritten by the default")

	require.NoError(t, f.mintAs(root.String(), "boot-bank", "boot-dep-2"))
}
