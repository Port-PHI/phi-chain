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

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

func importFixture(t *testing.T) (Keeper, sdk.Context, storetypes.StoreKey) {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_inst_import"))
	ctx := testCtx.Ctx
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	k := NewKeeper(cdc, key, "authority", nil, nil, nil, nil)
	return k, ctx, key
}

// A well-formed record under its own prefix is written at exactly the key it names.
func TestImportStoreEntriesWritesRecordsUnderTheExpectedPrefix(t *testing.T) {
	k, ctx, key := importFixture(t)

	wantKey := append(append([]byte(nil), types.DepositPrefix...), []byte("inst-1|mint|ref-1")...)
	err := k.importStoreEntries(ctx, types.DepositPrefix, []types.StoreEntry{
		{Key: wantKey, Value: []byte{types.DepositMarkerByte}},
	})
	require.NoError(t, err)
	require.Equal(t, []byte{types.DepositMarkerByte}, ctx.KVStore(key).Get(wantKey))
}

// The load-bearing case: a genesis entry declared as a deposit marker but keyed under ANOTHER prefix must be refused, not written.
func TestImportStoreEntriesRejectsAForeignKeyPrefix(t *testing.T) {
	k, ctx, key := importFixture(t)

	foreign := append(append([]byte(nil), types.InstitutionPrefix...), []byte("bank-of-nowhere")...)
	err := k.importStoreEntries(ctx, types.DepositPrefix, []types.StoreEntry{
		{Key: foreign, Value: []byte("forged institution record")},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not under the expected prefix")
	require.Nil(t, ctx.KVStore(key).Get(foreign), "a rejected entry must not be written")
}

// Rejection is per-batch, not per-entry: one bad key aborts before it, and InitGenesis panics on the error, so a partially-applied import cannot be committed.
func TestImportStoreEntriesRejectsTheWholeBatchOnOneBadKey(t *testing.T) {
	k, ctx, key := importFixture(t)

	good := append(append([]byte(nil), types.DepositPrefix...), []byte("ok")...)
	foreign := append(append([]byte(nil), types.ApprovalPrefix...), []byte("elsewhere")...)
	err := k.importStoreEntries(ctx, types.DepositPrefix, []types.StoreEntry{
		{Key: good, Value: []byte{types.DepositMarkerByte}},
		{Key: foreign, Value: []byte("nope")},
	})
	require.Error(t, err)
	require.Nil(t, ctx.KVStore(key).Get(foreign), "the offending entry is never written")
}

// A key equal to the bare prefix names no record.
func TestImportStoreEntriesRejectsABarePrefixKey(t *testing.T) {
	k, ctx, _ := importFixture(t)

	err := k.importStoreEntries(ctx, types.CounterPrefix, []types.StoreEntry{
		{Key: append([]byte(nil), types.CounterPrefix...), Value: []byte("1")},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not under the expected prefix")
}
