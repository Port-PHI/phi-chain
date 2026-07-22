// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

func starveFixture(t *testing.T, holders []sdk.AccAddress) fixture {
	t.Helper()
	dids := map[string]string{}
	for i, h := range holders {
		dids[h.String()] = fmt.Sprintf("did:phi:starve-%d", i)
	}
	f := setupDIDCap(t, "200000000", dids)
	f.registerAndAttest(t, "bank-a", 100_000_000)

	floor := types.CapInt(f.k.GetParams(f.ctx).RedeemFloorPerTx)
	require.True(t, floor.IsPositive(), "the protocol floor must be configured for this to mean anything")
	_, err := f.msg.UpdateInstitutionParams(f.ctx, &types.MsgUpdateInstitutionParams{
		Signer: f.admin.String(), Institution: "bank-a",
		Params: types.InstitutionParams{Caps: types.Caps{RedeemDaily: floor.String()}},
	})
	require.NoError(t, err)

	for i, h := range holders {
		f.mintTo(t, "bank-a", h, "100000", fmt.Sprintf("starve-dep-%d", i))
	}
	return f
}

func starveHolders(n int) []sdk.AccAddress {
	out := make([]sdk.AccAddress, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, sdk.AccAddress([]byte(fmt.Sprintf("%-20s", fmt.Sprintf("starve-holder-%d", i))[:20])))
	}
	return out
}

// The first holder consumes the whole institution-wide daily allowance; everyone else still reaches the floor.
func TestRedeemStarvation_OneHolderCannotStrandTheRest(t *testing.T) {
	holders := starveHolders(4)
	f := starveFixture(t, holders)
	floor := types.CapInt(f.k.GetParams(f.ctx).RedeemFloorPerTx)

	require.NoError(t, f.redeem("bank-a", holders[0], floor.String(), "red-first"),
		"the first holder consumes the whole institution-wide day")

	for i, h := range holders[1:] {
		require.NoError(t, f.redeem("bank-a", h, floor.String(), fmt.Sprintf("red-%d", i)),
			"holder %d must not be stranded by an allowance another holder already spent", i+1)
	}
}

// Past their own floor, a holder is held to the institution-wide cap like everyone else.
func TestRedeemStarvation_TheReservationIsBoundedAtOneFloor(t *testing.T) {
	holders := starveHolders(2)
	f := starveFixture(t, holders)
	floor := types.CapInt(f.k.GetParams(f.ctx).RedeemFloorPerTx)

	require.NoError(t, f.redeem("bank-a", holders[0], floor.String(), "red-a"))
	require.ErrorIs(t, f.redeem("bank-a", holders[0], floor.String(), "red-b"), types.ErrCapExceeded,
		"the reservation is one floor per subject per day, not an exemption from the cap")
}

// Every holder without an identity shares ONE subject bucket, so fresh addresses buy one reservation, not one each.
func TestRedeemStarvation_FreshAddressesShareASingleReservation(t *testing.T) {
	f := setupDIDCap(t, "200000000", map[string]string{})
	f.registerAndAttest(t, "bank-a", 100_000_000)

	floor := types.CapInt(f.k.GetParams(f.ctx).RedeemFloorPerTx)
	_, err := f.msg.UpdateInstitutionParams(f.ctx, &types.MsgUpdateInstitutionParams{
		Signer: f.admin.String(), Institution: "bank-a",
		Params: types.InstitutionParams{Caps: types.Caps{RedeemDaily: floor.String()}},
	})
	require.NoError(t, err)

	fresh := starveHolders(3)
	for i, h := range fresh {
		f.mintTo(t, "bank-a", h, "100000", fmt.Sprintf("fresh-dep-%d", i))
	}

	require.NoError(t, f.redeem("bank-a", fresh[0], floor.String(), "fresh-red-0"))

	for i, h := range fresh[1:] {
		require.ErrorIs(t, f.redeem("bank-a", h, floor.String(), fmt.Sprintf("fresh-red-%d", i+1)),
			types.ErrCapExceeded,
			"address %d must not buy a second reservation by being a different address", i+1)
	}
}

// The reservation relaxes the institution-wide daily cap and NOTHING else.
func TestRedeemStarvation_OtherCapsStillBind(t *testing.T) {
	holders := starveHolders(1)
	f := starveFixture(t, holders)
	floor := types.CapInt(f.k.GetParams(f.ctx).RedeemFloorPerTx)

	_, err := f.msg.UpdateInstitutionParams(f.ctx, &types.MsgUpdateInstitutionParams{
		Signer: f.admin.String(), Institution: "bank-a",
		Params: types.InstitutionParams{Caps: types.Caps{
			RedeemDaily: floor.String(), RedeemPerTx: floor.String(),
		}},
	})
	require.NoError(t, err)

	require.ErrorIs(t, f.redeem("bank-a", holders[0], floor.AddRaw(1).String(), "over-per-tx"),
		types.ErrCapExceeded, "the per-tx cap is untouched by the reservation")
}
