// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"encoding/binary"
	"testing"

	storetypes "cosmossdk.io/store/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

func removalEpoch(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

func (f fixture) removeAndDrain(t *testing.T, id string) {
	t.Helper()
	_, err := f.msg.RemoveInstitution(f.ctx, &types.MsgRemoveInstitution{Operator: f.oper.String(), Id: id})
	require.NoError(t, err)
	for i := 0; f.k.HasPendingRemoval(f.ctx, id); i++ {
		require.Less(t, i, 1_000_000, "removal sweep for %q did not drain", id)
		f.k.SweepRemovals(f.ctx)
	}
}

type removalFixture struct {
	f        fixture
	instID   string
	subAdmin sdk.AccAddress
	holder   sdk.AccAddress
	attestor sdk.AccAddress
}

func newRemovalFixture(t *testing.T) removalFixture {
	t.Helper()
	f := setup(t)
	r := removalFixture{
		f: f, instID: "wind-down",
		subAdmin: sdk.AccAddress([]byte("removal-subadmin____")),
		holder:   sdk.AccAddress([]byte("removal-holder______")),
		attestor: sdk.AccAddress([]byte("removal-attestor____")),
	}

	f.k.SetInstitution(f.ctx, types.Institution{
		Id: r.instID, Admin: sdk.AccAddress([]byte("removal-root-admin__")).String(),
		InstitutionType: types.INSTITUTION_TYPE_FINANCIAL,
		VaultBalance:    "0", AttestedReserve: "0",
	})
	f.k.SetRole(f.ctx, r.instID, r.subAdmin, types.INSTITUTION_ROLE_ADMIN)
	f.k.SetHolderKycTier(f.ctx, r.instID, r.holder, 3)
	f.k.SetLastAttestor(f.ctx, r.instID, r.attestor)

	store := f.ctx.KVStore(f.key)
	store.Set(types.ApprovalKey(r.instID, []byte("pending-action-hash"), r.subAdmin), removalEpoch(4))
	store.Set(types.AdminEpochKey(r.instID), removalEpoch(4))
	return r
}

// Removal clears every per-institution keyspace that grants or relaxes.
func TestRemoval_NoAuthorityGrantingStateSurvives(t *testing.T) {
	r := newRemovalFixture(t)
	store := r.f.ctx.KVStore(r.f.key)

	require.NotEmpty(t, store.Get(types.AdminEpochKey(r.instID)))
	require.NotEmpty(t, store.Get(types.LastAttestorKey(r.instID)))
	require.NotEmpty(t, store.Get(types.ApprovalKey(r.instID, []byte("pending-action-hash"), r.subAdmin)))
	require.Equal(t, types.INSTITUTION_ROLE_ADMIN, r.f.k.GetRole(r.f.ctx, r.instID, r.subAdmin))
	_, hadTier := r.f.k.HolderKycTier(r.f.ctx, r.instID, r.holder)
	require.True(t, hadTier)

	r.f.removeAndDrain(t, r.instID)

	require.False(t, r.f.k.HasInstitution(r.f.ctx, r.instID))
	require.Empty(t, store.Get(types.AdminEpochKey(r.instID)), "the admin epoch must not survive")
	require.Empty(t, store.Get(types.LastAttestorKey(r.instID)), "the reserve attestor must not survive")
	require.Empty(t, store.Get(types.ApprovalKey(r.instID, []byte("pending-action-hash"), r.subAdmin)),
		"a pending approval must not survive")

	require.Equal(t, types.INSTITUTION_ROLE_UNSPECIFIED, r.f.k.GetRole(r.f.ctx, r.instID, r.subAdmin),
		"a sub-admin's role must not survive the institution")
	_, tierSurvived := r.f.k.HolderKycTier(r.f.ctx, r.instID, r.holder)
	require.False(t, tierSurvived, "a holder's KYC tier must not survive the institution")

	for _, prefix := range types.PerInstitutionRangePrefixes(r.instID) {
		it := storetypes.KVStorePrefixIterator(store, prefix)
		require.False(t, it.Valid(), "a record survived under prefix %X", prefix)
		_ = it.Close()
	}
}

// The consequence: a re-registration of the same id inherits nothing.
func TestRemoval_ARegistrationOfTheSameIDInheritsNothing(t *testing.T) {
	r := newRemovalFixture(t)
	r.f.removeAndDrain(t, r.instID)

	freshAdmin := sdk.AccAddress([]byte("fresh-root-admin____"))
	r.f.k.SetInstitution(r.f.ctx, types.Institution{
		Id: r.instID, Admin: freshAdmin.String(),
		InstitutionType: types.INSTITUTION_TYPE_FINANCIAL,
		VaultBalance:    "0", AttestedReserve: "0",
	})

	_, hasAttestor := r.f.k.LastAttestor(r.f.ctx, r.instID)
	require.False(t, hasAttestor,
		"a fresh institution must not begin with an attestation made by an entity that no longer exists")
	require.Equal(t, types.INSTITUTION_ROLE_UNSPECIFIED, r.f.k.GetRole(r.f.ctx, r.instID, r.subAdmin),
		"a removed sub-admin must not return with their role")
	_, hasTier := r.f.k.HolderKycTier(r.f.ctx, r.instID, r.holder)
	require.False(t, hasTier)
	require.Empty(t, r.f.ctx.KVStore(r.f.key).Get(types.AdminEpochKey(r.instID)),
		"the epoch must start fresh, not continue the removed institution's")
}

// State that RESTRICTS (anti-replay markers, daily cap counters) must survive removal, or removal undoes it.
func TestRemoval_ReplayAndCapStateDeliberatelySurvives(t *testing.T) {
	r := newRemovalFixture(t)
	store := r.f.ctx.KVStore(r.f.key)

	depositKey := types.DepositKey(r.instID, "in", "processed-ref")
	counterKey := types.CounterTotalKey(r.instID, "mint", 19_000)
	store.Set(depositKey, []byte{types.DepositMarkerByte})
	store.Set(counterKey, []byte("5000"))

	r.f.removeAndDrain(t, r.instID)

	require.NotEmpty(t, store.Get(depositKey),
		"removal must not un-burn an anti-replay marker; the id is reusable and the deposit was real")
	require.NotEmpty(t, store.Get(counterKey),
		"removal must not reset a consumed daily cap")
}
