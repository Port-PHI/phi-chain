// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

// The mint-pause gate is enforced.
func TestMint_RejectsWhenPausedMintIsSet(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 1000)

	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(),
		AmountToman: "100", DepositRef: "dep-before",
	})
	require.NoError(t, err)

	inst, found := f.k.GetInstitution(f.ctx, "bank-a")
	require.True(t, found)
	inst.PausedMint = true
	f.k.SetInstitution(f.ctx, inst)

	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(),
		AmountToman: "100", DepositRef: "dep-after",
	})
	require.ErrorIs(t, err, types.ErrMintPaused)
}

// The one-directional safety property: a paused institution still honours redemptions.
func TestRedeem_IsUnaffectedByPausedMint(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 1000)
	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(),
		AmountToman: "1000", DepositRef: "dep-1",
	})
	require.NoError(t, err)

	inst, found := f.k.GetInstitution(f.ctx, "bank-a")
	require.True(t, found)
	inst.PausedMint = true
	f.k.SetInstitution(f.ctx, inst)

	_, err = f.msg.InstitutionRedeem(f.ctx, &types.MsgInstitutionRedeem{
		Admin: f.holder.String(), Institution: "bank-a", Holder: f.holder.String(),
		AmountToman: "400", RedeemRef: "red-1",
	})
	require.NoError(t, err, "redemption must stay open while minting is paused")
}

// A genesis-paused institution starts paused: the flag survives import, which is the only way it is set today.
func TestGenesisCanStartAnInstitutionPaused(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 1000)

	gs := f.k.ExportGenesis(f.ctx)
	for i := range gs.Institutions {
		if gs.Institutions[i].Id == "bank-a" {
			gs.Institutions[i].PausedMint = true
		}
	}

	g := setup(t)
	require.NotPanics(t, func() { g.k.InitGenesis(g.ctx, *gs) })

	imported, found := g.k.GetInstitution(g.ctx, "bank-a")
	require.True(t, found)
	require.True(t, imported.PausedMint, "the pause flag must survive genesis import")

	_, err := g.msg.InstitutionMint(g.ctx, &types.MsgInstitutionMint{
		Admin: g.admin.String(), Institution: "bank-a", Recipient: g.holder.String(),
		AmountToman: "10", DepositRef: "dep-x",
	})
	require.ErrorIs(t, err, types.ErrMintPaused)
}
