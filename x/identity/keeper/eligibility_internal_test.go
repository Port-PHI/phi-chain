// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/phicrypto"
	"github.com/Port-PHI/phi-chain/x/identity/types"
)

func setupInternal(t *testing.T) (sdk.Context, Keeper) {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_id_internal"))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	k := NewKeeper(cdc, key, sdk.AccAddress([]byte("gov_authority_______")).String(), phicrypto.AcceptAll(), nil)
	return testCtx.Ctx, k
}

func seedActive(k Keeper, ctx sdk.Context, did, controller string, createdAt int64) {
	k.SetIdentity(ctx, types.DIDDocument{
		Did: did, Controller: controller, Status: types.DID_STATUS_ACTIVE,
		CreatedAt: createdAt, PubKey: []byte("pk"), UniquenessHash: []byte("uniq-" + did),
	})
}

func TestEligibilityInvariant_DetectsWrongRecordValue(t *testing.T) {
	ctx, k := setupInternal(t)
	seedActive(k, ctx, "did:phi:a1", "ctrlA", 50)
	if msg, broken := EligibilityIndexInvariant(k)(ctx); broken {
		t.Fatalf("invariant must hold on freshly-built state: %s", msg)
	}

	ctx.KVStore(k.storeKey).Set(types.ControllerEligibilityKey("ctrlA"), types.SortableInt64(999))

	_, broken := EligibilityIndexInvariant(k)(ctx)
	require.True(t, broken, "a record disagreeing with the identity records must break the invariant")
}

func TestEligibilityInvariant_DetectsWrongTotal(t *testing.T) {
	ctx, k := setupInternal(t)
	seedActive(k, ctx, "did:phi:a1", "ctrlA", 50)

	k.setEligibleControllerTotal(ctx, 42)

	_, broken := EligibilityIndexInvariant(k)(ctx)
	require.True(t, broken, "a total disagreeing with the record count must break the invariant")
}

func TestEligibilityInvariant_DetectsOrphanRecord(t *testing.T) {
	ctx, k := setupInternal(t)
	seedActive(k, ctx, "did:phi:a1", "ctrlA", 50)

	store := ctx.KVStore(k.storeKey)
	store.Set(types.ControllerEligibilityKey("ctrlGhost"), types.SortableInt64(10))
	store.Set(types.EligibilityByAgeKey(10, "ctrlGhost"), []byte{1})
	k.setEligibleControllerTotal(ctx, 2)

	_, broken := EligibilityIndexInvariant(k)(ctx)
	require.True(t, broken, "a record without an ACTIVE DID must break the invariant")
}

func TestEligibilityInvariant_DetectsMissingMirrorEntry(t *testing.T) {
	ctx, k := setupInternal(t)
	seedActive(k, ctx, "did:phi:a1", "ctrlA", 50)

	ctx.KVStore(k.storeKey).Delete(types.EligibilityByAgeKey(50, "ctrlA"))

	_, broken := EligibilityIndexInvariant(k)(ctx)
	require.True(t, broken, "an unmirrored eligibility record must break the invariant")
}

// The age-ordered mirror is what the denominator's tail scan walks; a stray entry there would over-count the tail and under-report the denominator.
func TestEligibilityInvariant_DetectsStrayMirrorEntry(t *testing.T) {
	ctx, k := setupInternal(t)
	seedActive(k, ctx, "did:phi:a1", "ctrlA", 50)

	ctx.KVStore(k.storeKey).Set(types.EligibilityByAgeKey(77, "ctrlStray"), []byte{1})

	_, broken := EligibilityIndexInvariant(k)(ctx)
	require.True(t, broken, "a mirror entry with no matching record must break the invariant")
}
