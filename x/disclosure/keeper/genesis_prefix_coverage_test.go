// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/internal/storeprefix/prefixtest"
	"github.com/Port-PHI/phi-chain/phicrypto"
	"github.com/Port-PHI/phi-chain/x/disclosure/keeper"
	"github.com/Port-PHI/phi-chain/x/disclosure/types"
)

func disclosureStore(t *testing.T, name string) (sdk.Context, keeper.Keeper, storetypes.StoreKey) {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey(name))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	authority := sdk.AccAddress([]byte("gov_authority_______")).String()
	k := keeper.NewKeeper(cdc, key, authority, newFakeCredentials(), phicrypto.AcceptAll())
	require.NoError(t, k.SetParams(testCtx.Ctx, types.DefaultParams()))
	return testCtx.Ctx, k, key
}

// TestGenesis_RoundTripsEveryDeclaredStorePrefix asserts one export→import cycle reproduces the module's whole keyspace, and that the keyspace is nothing but the declaration says it is.
func TestGenesis_RoundTripsEveryDeclaredStorePrefix(t *testing.T) {
	ctx, k, key := disclosureStore(t, "t_disc_cov")

	p := k.GetParams(ctx)
	p.MaxProofSizeBytes = p.MaxProofSizeBytes - 1
	require.NoError(t, k.SetParams(ctx, p))

	before := prefixtest.Dump(ctx, key)
	prefixtest.RequireSeeded(t, before, types.AllStorePrefixes())

	exported := k.ExportGenesis(ctx)
	require.NoError(t, exported.Validate())

	ctx2, k2, key2 := disclosureStore(t, "t_disc_cov2")
	require.NotPanics(t, func() { k2.InitGenesis(ctx2, *exported) })

	prefixtest.RequireRoundTrip(t, types.AllStorePrefixes(), before, prefixtest.Dump(ctx2, key2))
}
