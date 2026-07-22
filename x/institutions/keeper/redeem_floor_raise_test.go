// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

func floorRaiseFixture(t *testing.T, originalFloor, raisedFloor string) (fixture, sdk.AccAddress) {
	t.Helper()
	holder := sdk.AccAddress([]byte("floor-raise-holder__"))
	f := setupDIDCap(t, "200000000", map[string]string{holder.String(): "did:phi:floor-raise"})

	p := f.k.GetParams(f.ctx)
	p.RedeemFloorPerTx = originalFloor
	require.NoError(t, f.k.SetParams(f.ctx, p))

	f.registerAndAttest(t, "bank-a", 100_000_000)
	_, err := f.msg.UpdateInstitutionParams(f.ctx, &types.MsgUpdateInstitutionParams{
		Signer: f.admin.String(), Institution: "bank-a",
		Params: types.InstitutionParams{Caps: types.Caps{
			RedeemPerTx: originalFloor, RedeemDaily: originalFloor, RedeemPerUser: originalFloor,
		}},
	})
	require.NoError(t, err, "caps at the floor of the day must install cleanly")

	f.mintTo(t, "bank-a", holder, "100000", "floor-raise-dep")

	p = f.k.GetParams(f.ctx)
	p.RedeemFloorPerTx = raisedFloor
	require.NoError(t, f.k.SetParams(f.ctx, p))

	return f, holder
}

// TestFloorRaise_ExistingInstitutionsAreBoundByTheNewFloor is the case the fix exists for: a stored cap that is now below the floor is treated as the floor, not as its stale value.
func TestFloorRaise_ExistingInstitutionsAreBoundByTheNewFloor(t *testing.T) {
	f, holder := floorRaiseFixture(t, "100", "500")

	inst, ok := f.k.GetInstitution(f.ctx, "bank-a")
	require.True(t, ok)
	require.Equal(t, "100", inst.Params.Caps.RedeemPerTx, "the stored cap is deliberately left stale")

	require.NoError(t, f.redeem("bank-a", holder, "500", "red-1"),
		"a raised floor must apply to an institution registered under the old one")
}

// Every redeem cap is read through the floor, not just the per-tx one — the per-tx, daily and per-user caps are each a way to strand a holder below it.
func TestFloorRaise_EveryRedeemCapIsReadThroughTheFloor(t *testing.T) {
	for _, tc := range []struct {
		name string
		caps types.Caps
	}{
		{"per-tx alone", types.Caps{RedeemPerTx: "100"}},
		{"daily alone", types.Caps{RedeemDaily: "100"}},
		{"per-user alone", types.Caps{RedeemPerUser: "100"}},
		{"all three", types.Caps{RedeemPerTx: "100", RedeemDaily: "100", RedeemPerUser: "100"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			holder := sdk.AccAddress([]byte("floor-raise-holder__"))
			f := setupDIDCap(t, "200000000", map[string]string{holder.String(): "did:phi:floor-raise"})

			p := f.k.GetParams(f.ctx)
			p.RedeemFloorPerTx = "100"
			require.NoError(t, f.k.SetParams(f.ctx, p))

			f.registerAndAttest(t, "bank-a", 100_000_000)
			_, err := f.msg.UpdateInstitutionParams(f.ctx, &types.MsgUpdateInstitutionParams{
				Signer: f.admin.String(), Institution: "bank-a",
				Params: types.InstitutionParams{Caps: tc.caps},
			})
			require.NoError(t, err)
			f.mintTo(t, "bank-a", holder, "100000", "dep-1")

			p = f.k.GetParams(f.ctx)
			p.RedeemFloorPerTx = "500"
			require.NoError(t, f.k.SetParams(f.ctx, p))

			require.NoError(t, f.redeem("bank-a", holder, "500", "red-1"),
				"%s must be raised to the new floor", tc.name)
		})
	}
}

// The floor RAISES caps; it never imposes one.
func TestFloorRaise_AboveFloorAndUncappedAreUntouched(t *testing.T) {
	require.Equal(t, "0", types.AtLeastFloor(types.CapInt(""), types.CapInt("500")).String(),
		"an unset cap means uncapped and must stay uncapped")
	require.Equal(t, "900", types.AtLeastFloor(types.CapInt("900"), types.CapInt("500")).String(),
		"a cap already above the floor is left alone")
	require.Equal(t, "500", types.AtLeastFloor(types.CapInt("100"), types.CapInt("500")).String(),
		"a sub-floor cap reads as the floor")
	require.Equal(t, "100", types.AtLeastFloor(types.CapInt("100"), types.CapInt("0")).String(),
		"with no floor configured nothing is raised")
}

// Lowering the floor does not lower a cap the institution set for itself: the floor is a lower bound on what a holder may redeem, never an upper bound on what an institution may permit.
func TestFloorRaise_LoweringTheFloorDoesNotShrinkAnInstitutionsCap(t *testing.T) {
	f, holder := floorRaiseFixture(t, "100", "50")

	require.NoError(t, f.redeem("bank-a", holder, "100", "red-1"))
	require.ErrorIs(t, f.redeem("bank-a", holder, "1", "red-2"), types.ErrCapExceeded,
		"the institution's own cap still binds above the floor")
}
