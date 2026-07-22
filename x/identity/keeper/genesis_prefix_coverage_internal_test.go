// SPDX-License-Identifier: Apache-2.0

package keeper

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
	"github.com/Port-PHI/phi-chain/x/identity/types"
)

func coverageStore(t *testing.T, name string) (sdk.Context, Keeper, storetypes.StoreKey) {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey(name))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	k := NewKeeper(cdc, key, sdk.AccAddress([]byte("gov_authority_______")).String(),
		phicrypto.AcceptAll(), nil)
	ctx := testCtx.Ctx.WithChainID("phi-testnet-1").WithBlockTime(time.Unix(1_000_000, 0))
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))
	return ctx, k, key
}

// TestGenesis_RoundTripsEveryDeclaredStorePrefix seeds a record under every declared prefix through the real keeper writers and asserts one export→import cycle reproduces the module's whole keyspace.
func TestGenesis_RoundTripsEveryDeclaredStorePrefix(t *testing.T) {
	ctx, k, key := coverageStore(t, "t_id_cov")
	now := ctx.BlockTime().Unix()

	const (
		ownerDID  = "did:phi:owner"
		valDID    = "did:phi:validator"
		issuerDID = "did:phi:issuer"
	)
	ownerCtrl := sdk.AccAddress([]byte("owner-controller____")).String()
	valCtrl := sdk.AccAddress([]byte("validator-controller")).String()
	newCtrl := sdk.AccAddress([]byte("new-device-account__")).String()
	valoper := sdk.ValAddress([]byte("validator-operator__")).String()
	recoveryNonce := []byte("recovery-nonce-1")
	proposedKey := []byte("proposed-key")
	recoveryID := types.DeriveRecoveryID(ownerDID, proposedKey, recoveryNonce)

	for _, d := range []types.DIDDocument{
		{Did: ownerDID, Controller: ownerCtrl, Status: types.DID_STATUS_ACTIVE,
			CreatedAt: now - 100_000, PubKey: []byte("pk-owner"), UniquenessHash: []byte("uniq-owner")},
		{Did: valDID, Controller: valCtrl, Status: types.DID_STATUS_ACTIVE,
			CreatedAt: now - 50_000, PubKey: []byte("pk-validator"), UniquenessHash: []byte("uniq-validator")},
	} {
		k.SetIdentity(ctx, d)
		k.setUniqueness(ctx, d.UniquenessHash, d.Did)
	}
	k.SetIdentityCount(ctx, 2)
	k.SetTrustedIssuer(ctx, types.TrustedIssuer{Did: issuerDID, PubKey: []byte("issuer-pk"), Active: true})
	k.markIssuerNonce(ctx, issuerDID, []byte("attestation-nonce-1"))
	k.BindValidatorToDID(ctx, valDID, valoper)
	k.SetGuardianSet(ctx, types.GuardianSet{
		Did: ownerDID, Threshold: 3,
		Commitments: [][]byte{
			make([]byte, 32), append(make([]byte, 31), 1), append(make([]byte, 31), 2),
			append(make([]byte, 31), 3), append(make([]byte, 31), 4),
		},
	})
	k.bumpGuardianEpoch(ctx, ownerDID)
	k.SetRecoveryRequest(ctx, types.RecoveryRequest{
		RecoveryId: recoveryID, Did: ownerDID, ProposedNewController: newCtrl,
		ProposedNewPubKey: proposedKey, KeyType: types.KEY_TYPE_SECP256R1,
		Method: types.RECOVERY_METHOD_SOCIAL, Status: types.RECOVERY_STATUS_PENDING,
		Nonce:       recoveryNonce,
		InitiatedAt: now, ExecuteAfter: now + 72*3600, ExpiresAt: now + 96*3600,
		DepositUphi: "1000000", FeeUphi: "1000",
	})
	k.setRecoveryTallyEpoch(ctx, recoveryID, k.GuardianEpoch(ctx, ownerDID))
	k.markRecoveryNonce(ctx, ownerDID, recoveryNonce)

	before := prefixtest.Dump(ctx, key)
	prefixtest.RequireSeeded(t, before, types.AllStorePrefixes())

	exported := k.ExportGenesis(ctx)
	require.NoError(t, exported.Validate())

	ctx2, k2, key2 := coverageStore(t, "t_id_cov2")
	require.NotPanics(t, func() { k2.InitGenesis(ctx2, *exported) })

	prefixtest.RequireRoundTrip(t, types.AllStorePrefixes(), before, prefixtest.Dump(ctx2, key2))
}

// TestGenesis_RoundTripKeepsRetiredApprovalsRetired is the targeted case behind the epoch value check.
func TestGenesis_RoundTripKeepsRetiredApprovalsRetired(t *testing.T) {
	ctx, k, _ := coverageStore(t, "t_id_epoch")
	now := ctx.BlockTime().Unix()

	const did = "did:phi:epoch-owner"
	ctrl := sdk.AccAddress([]byte("epoch-controller____")).String()
	k.SetIdentity(ctx, identityDoc(did, ctrl, now))
	k.setUniqueness(ctx, []byte("uniq-epoch-owner"), did)
	k.SetIdentityCount(ctx, 1)
	k.SetGuardianSet(ctx, types.GuardianSet{
		Did: did, Threshold: 2,
		Commitments: [][]byte{make([]byte, 32), append(make([]byte, 31), 1), append(make([]byte, 31), 2)},
	})

	nonce := []byte("epoch-recovery-nonce")
	newKey := []byte("epoch-proposed-key")
	recoveryID := types.DeriveRecoveryID(did, newKey, nonce)
	k.SetRecoveryRequest(ctx, types.RecoveryRequest{
		RecoveryId: recoveryID, Did: did, ProposedNewPubKey: newKey,
		ProposedNewController: sdk.AccAddress([]byte("epoch-new-device____")).String(),
		KeyType:               types.KEY_TYPE_SECP256R1, Method: types.RECOVERY_METHOD_SOCIAL,
		Status: types.RECOVERY_STATUS_PENDING, Nonce: nonce,
		InitiatedAt: now, ExecuteAfter: now + 72*3600, ExpiresAt: now + 96*3600,
		DepositUphi: "1000000", FeeUphi: "1000",
	})
	k.setRecoveryTallyEpoch(ctx, recoveryID, k.GuardianEpoch(ctx, did))
	k.bumpGuardianEpoch(ctx, did)
	require.Equal(t, uint64(1), k.GuardianEpoch(ctx, did))
	require.NotEqual(t, k.GuardianEpoch(ctx, did), k.recoveryTallyEpoch(ctx, recoveryID),
		"precondition: the tally is retired before the round-trip")

	exported := k.ExportGenesis(ctx)
	require.NoError(t, exported.Validate())

	ctx2, k2, _ := coverageStore(t, "t_id_epoch2")
	require.NotPanics(t, func() { k2.InitGenesis(ctx2, *exported) })

	require.Equal(t, uint64(1), k2.GuardianEpoch(ctx2, did),
		"the guardian-set epoch must survive genesis, or every retired approval revives")
	require.Equal(t, uint64(0), k2.recoveryTallyEpoch(ctx2, recoveryID),
		"the tally must be imported with the epoch it was actually collected under")
	require.NotEqual(t, k2.GuardianEpoch(ctx2, did), k2.recoveryTallyEpoch(ctx2, recoveryID),
		"a rotation that retired a tally must stay retired across a restart")
}

func identityDoc(did, controller string, createdAt int64) types.DIDDocument {
	return types.DIDDocument{
		Did: did, Controller: controller, PubKey: []byte("pk-" + did),
		UniquenessHash: []byte("uniq-epoch-owner"), Status: types.DID_STATUS_ACTIVE,
		CreatedAt: createdAt,
	}
}
