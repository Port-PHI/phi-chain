// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

// The admin count is what the two-distinct-admins mint rule and the sensitive-action threshold both consult, so anything it counts that is not a signable key weakens both.
func TestCountAdmins_OnlyCountsRealKeys(t *testing.T) {
	realA := sdk.AccAddress([]byte("count-admin-a_______")).String()
	realB := sdk.AccAddress([]byte("count-admin-b_______")).String()

	for _, tc := range []struct {
		name   string
		admin  string
		grants []string
		want   uint32
	}{
		{"one real root admin", realA, nil, 1},
		{"root admin plus a distinct grant", realA, []string{realB}, 2},
		{"a grant duplicating the root admin counts once", realA, []string{realA}, 1},
		{"a blank root admin counts for nothing", "", []string{realA}, 1},
		{"a malformed root admin counts for nothing", "not-an-address", []string{realA}, 1},
		{"a malformed grant counts for nothing", realA, []string{"also-not-an-address"}, 1},
		{"nothing real at all", "", []string{"garbage"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k, ctx, _ := importFixture(t)
			inst := types.Institution{Id: "bank-a", Admin: tc.admin}
			k.SetInstitution(ctx, inst)
			for _, g := range tc.grants {
				if addr, err := sdk.AccAddressFromBech32(g); err == nil {
					k.SetRole(ctx, "bank-a", addr, types.INSTITUTION_ROLE_ADMIN)
					continue
				}
				ctx.KVStore(k.storeKey).Set(
					append(append([]byte{}, types.RolePrefix...), []byte("\x06bank-a"+g)...),
					k.cdc.MustMarshal(&types.RoleGrant{
						Institution: "bank-a", Address: g, Role: types.INSTITUTION_ROLE_ADMIN,
					}))
			}

			require.Equal(t, tc.want, k.countAdmins(ctx, inst))
		})
	}
}

// The rule the count exists for: a phantom must not carry an institution over the mint threshold.
func TestCountAdmins_APhantomDoesNotReachTheMintThreshold(t *testing.T) {
	k, ctx, _ := importFixture(t)
	real := sdk.AccAddress([]byte("only-real-admin_____")).String()

	inst := types.Institution{Id: "bank-a", Admin: ""}
	k.SetInstitution(ctx, inst)
	realAddr, err := sdk.AccAddressFromBech32(real)
	require.NoError(t, err)
	k.SetRole(ctx, "bank-a", realAddr, types.INSTITUTION_ROLE_ADMIN)

	require.Less(t, k.countAdmins(ctx, inst), types.MinAdminsForMint,
		"a blank admin plus one real key is one key, and one key must not satisfy a two-key rule")
}
