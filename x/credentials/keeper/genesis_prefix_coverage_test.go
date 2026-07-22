// SPDX-License-Identifier: Apache-2.0

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
	"github.com/Port-PHI/phi-chain/x/credentials/keeper"
	"github.com/Port-PHI/phi-chain/x/credentials/types"
)

func credentialsStore(t *testing.T, name string) (sdk.Context, keeper.Keeper, storetypes.StoreKey) {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey(name))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	authority := sdk.AccAddress([]byte("gov_authority_______")).String()
	k := keeper.NewKeeper(cdc, key, authority, newFakeIdentity(), phicrypto.AcceptAll())
	ctx := testCtx.Ctx.WithBlockTime(time.Unix(1_000_000, 0))
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))
	return ctx, k, key
}

func TestGenesis_RoundTripsEveryDeclaredStorePrefix(t *testing.T) {
	ctx, k, key := credentialsStore(t, "t_cred_cov")

	k.SetTemplate(ctx, types.CredentialTemplate{
		Id: "phi.kyc.v1", Version: 1, OwnerDid: issuerDID,
		SchemaHash: []byte("schema-v1"), Name: "KYC",
		IssuerBbsPubkey: []byte("issuer-bbs-pubkey"),
		MessageCount:    4, DisclosableIndices: []uint32{1, 2},
		DisclosurePolicyHash: types.DisclosurePolicyHash("phi.kyc.v1", 1, 4, []uint32{1, 2}),
		Status:               types.TEMPLATE_STATUS_ACTIVE,
	})
	k.SetAnchor(ctx, types.CredentialAnchor{
		CredentialHash: []byte("credential-hash-0001"),
		TemplateId:     "phi.kyc.v1", IssuerDid: issuerDID, SubjectDid: subjectDID,
		IssuedAt: ctx.BlockTime().Unix(),
	})
	k.SetAgreement(ctx, types.Agreement{
		Hash: []byte("agreement-hash-0001"), Creator: acc(aliceAddr),
		RequiredSigners: []string{aliceDID, bobDID},
		Status:          types.AGREEMENT_STATUS_PENDING, CreatedAt: ctx.BlockTime().Unix(),
	})
	k.SetPersonalAnchor(ctx, types.PersonalAnchor{
		OwnerDid: aliceDID, AnchorHash: []byte("personal-hash-0001"),
		AnchoredAt: ctx.BlockTime().Unix(),
	})

	before := prefixtest.Dump(ctx, key)
	prefixtest.RequireSeeded(t, before, types.AllStorePrefixes())

	exported := k.ExportGenesis(ctx)
	require.NoError(t, exported.Validate())

	ctx2, k2, key2 := credentialsStore(t, "t_cred_cov2")
	require.NotPanics(t, func() { k2.InitGenesis(ctx2, *exported) })

	prefixtest.RequireRoundTrip(t, types.AllStorePrefixes(), before, prefixtest.Dump(ctx2, key2))
}
