// SPDX-License-Identifier: Apache-2.0

//go:build !voting_snark

package keeper_test

import (
	"testing"
	"time"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/internal/storeprefix/prefixtest"
	"github.com/Port-PHI/phi-chain/phicrypto"
	credentialstypes "github.com/Port-PHI/phi-chain/x/credentials/types"
	"github.com/Port-PHI/phi-chain/x/voting/keeper"
	"github.com/Port-PHI/phi-chain/x/voting/types"
)

func votingStore(t *testing.T, name string) (sdk.Context, keeper.Keeper, storetypes.StoreKey) {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey(name))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	creds := &fakeCredentials{templates: map[string]credentialstypes.CredentialTemplate{}}
	creds.addTemplate(tmplID, issuerDID, []byte("issuer-bbs-pubkey"))
	authority := sdk.AccAddress([]byte("gov_authority_______")).String()
	k := keeper.NewKeeper(cdc, key, authority, creds, phicrypto.AcceptAll(), true)
	ctx := testCtx.Ctx.WithBlockTime(time.Unix(1_000_000, 0))
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))
	return ctx, k, key
}

// TestGenesis_RoundTripsEveryDeclaredStorePrefix seeds a record under every declared prefix through the real keeper writers and asserts one export→import cycle reproduces the module's whole keyspace.
func TestGenesis_RoundTripsEveryDeclaredStorePrefix(t *testing.T) {
	ctx, k, key := votingStore(t, "t_vote_cov")
	now := ctx.BlockTime().Unix()

	k.SetElection(ctx, types.Election{
		Id: "e-1", Title: "Q?", Options: []string{"yes", "no"},
		RequiredTemplateId: tmplID, Creator: acc(creatorAddr),
		VotingStart: 0, VotingEnd: now + 86400,
		Status:        types.ELECTION_STATUS_OPEN,
		OptionTallies: []uint64{1, 1}, TotalVotes: 2,
	})
	for i, n := range []string{"nullifier-0000000001", "nullifier-0000000002"} {
		k.SetBallot(ctx, types.Ballot{
			ElectionId: "e-1", Nullifier: []byte(n),
			OptionIndex: uint32(i % 2), CastAt: now,
		})
	}

	before := prefixtest.Dump(ctx, key)
	prefixtest.RequireSeeded(t, before, types.AllStorePrefixes())

	exported := k.ExportGenesis(ctx)
	require.NoError(t, exported.Validate())

	ctx2, k2, key2 := votingStore(t, "t_vote_cov2")
	require.NotPanics(t, func() { k2.InitGenesis(ctx2, *exported) })

	prefixtest.RequireRoundTrip(t, types.AllStorePrefixes(), before, prefixtest.Dump(ctx2, key2))
}
