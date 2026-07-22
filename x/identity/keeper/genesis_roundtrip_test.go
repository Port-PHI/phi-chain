// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"fmt"
	"testing"
	"time"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/keeper"
	"github.com/Port-PHI/phi-chain/x/identity/types"
)

func freshIdentityKeeper(t *testing.T) (sdk.Context, keeper.Keeper) {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_id_import"))
	ctx := testCtx.Ctx.WithChainID(testChainID).WithBlockTime(time.Unix(1_000_000, 0))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	authority := sdk.AccAddress([]byte("gov_authority_______")).String()
	return ctx, keeper.NewKeeper(cdc, key, authority, phicryptoAcceptAll(), newFakeBank())
}

type didStateCase struct {
	name        string
	status      types.DIDStatus
	guardians   bool
	recovery    bool
	reachable   bool
	unreachable string // why the runtime cannot produce this cell
}

func didStateMatrix() []didStateCase {
	return []didStateCase{
		{name: "active/no-guardians/no-recovery", status: types.DID_STATUS_ACTIVE, reachable: true},
		{
			name: "active/no-guardians/open-recovery", status: types.DID_STATUS_ACTIVE, recovery: true,
			unreachable: "SOCIAL initiate requires a guardian set and REAUTH is compiled inert",
		},
		{name: "active/guardians/no-recovery", status: types.DID_STATUS_ACTIVE, guardians: true, reachable: true},
		{name: "active/guardians/open-recovery", status: types.DID_STATUS_ACTIVE, guardians: true, recovery: true, reachable: true},

		{name: "suspended/no-guardians/no-recovery", status: types.DID_STATUS_SUSPENDED, reachable: true},
		{
			name: "suspended/no-guardians/open-recovery", status: types.DID_STATUS_SUSPENDED, recovery: true,
			unreachable: "no request can be opened without a guardian set, and suspension never removes one",
		},
		{name: "suspended/guardians/no-recovery", status: types.DID_STATUS_SUSPENDED, guardians: true, reachable: true},
		{name: "suspended/guardians/open-recovery", status: types.DID_STATUS_SUSPENDED, guardians: true, recovery: true, reachable: true},

		{name: "revoked/no-guardians/no-recovery", status: types.DID_STATUS_REVOKED, reachable: true},
		{
			name: "revoked/no-guardians/open-recovery", status: types.DID_STATUS_REVOKED, recovery: true,
			unreachable: "revocation terminates every open request",
		},
		{
			name: "revoked/guardians/no-recovery", status: types.DID_STATUS_REVOKED, guardians: true,
			unreachable: "revocation clears the guardian set",
		},
		{
			name: "revoked/guardians/open-recovery", status: types.DID_STATUS_REVOKED, guardians: true, recovery: true,
			unreachable: "revocation clears the guardian set and terminates every open request",
		},
	}
}

// export and the validation provably agree on every state the chain can reach.
// TestGenesis_RoundTripsEveryReachableDIDState drives the FULL matrix of DID states — each of the three statuses crossed with having/not having a guardian set and having/not having an open recovery request — into one chain, then asserts that a single export→Validate→import→export cycle is lossless and panic-free for all of them at once.
func TestGenesis_RoundTripsEveryReachableDIDState(t *testing.T) {
	ctx, k, msg, bank := setupIdentityFull(t, phicryptoAcceptAll())
	now := time.Unix(1_000_000, 0)
	ctx = ctx.WithBlockTime(now)
	auth := k.GetAuthority()

	guardianDIDs, commitments := guardianPool(t, ctx, msg, 5)
	require.Len(t, guardianDIDs, 5)

	for i, tc := range didStateMatrix() {
		t.Run(tc.name, func(t *testing.T) {
			label := fmt.Sprintf("subject-%d", i)
			ctrl := someAddr(fmt.Sprintf("subject-ctrl-%-7d", i))
			did := registerActive(t, ctx, msg, ctrl, label, []byte("bio-"+label))

			if tc.guardians {
				_, err := msg.SetGuardians(ctx, &types.MsgSetGuardians{
					Controller: ctrl, Did: did, Commitments: commitments, Threshold: 3,
				})
				require.NoError(t, err)
			}
			if tc.recovery {
				initiator := someAddr(fmt.Sprintf("subject-new-%-8d", i))
				addr, err := sdk.AccAddressFromBech32(initiator)
				require.NoError(t, err)
				bank.Fund(addr, k.GetParams(ctx).RecoveryDeposit().MulRaw(4))
				_, err = msg.InitiateRecovery(ctx, &types.MsgInitiateRecovery{
					Creator:           initiator,
					Did:               did,
					ProposedNewPubKey: pubFor("recovered-" + label),
					KeyType:           types.KEY_TYPE_SECP256R1,
					Method:            types.RECOVERY_METHOD_SOCIAL,
					Nonce:             []byte("nonce-" + label),
					PopSig:            []byte("pop"),
				})
				if !tc.guardians {
					require.Error(t, err, tc.unreachable)
					return
				}
				require.NoError(t, err)
			}

			switch tc.status {
			case types.DID_STATUS_SUSPENDED:
				_, err := msg.UpdateStatus(ctx, &types.MsgUpdateStatus{
					Authority: auth, Did: did, NewStatus: types.DID_STATUS_SUSPENDED,
				})
				require.NoError(t, err)
			case types.DID_STATUS_REVOKED:
				beforeRevoke := bank.Total()
				_, err := msg.RevokeIdentity(ctx, &types.MsgRevokeIdentity{Creator: ctrl, Did: did})
				require.NoError(t, err)
				require.True(t, bank.Total().Equal(beforeRevoke), "revocation must be supply-neutral")
			}

			_, hasGuardians := k.GetGuardians(ctx, did)
			openRequests := len(k.RecoveryRequestsForDID(ctx, did))
			if tc.status == types.DID_STATUS_REVOKED {
				require.False(t, hasGuardians, "a revoked DID must keep no guardian set: %s", tc.unreachable)
				require.Zero(t, openRequests, "a revoked DID must keep no open recovery request: %s", tc.unreachable)
				return
			}
			require.True(t, tc.reachable, "cell should have been unreachable: %s", tc.unreachable)
			require.Equal(t, tc.guardians, hasGuardians)
			require.Equal(t, tc.recovery, openRequests > 0)
		})
	}

	if inv, broken := keeper.AllInvariants(k)(ctx); broken {
		t.Fatalf("built state breaks an identity invariant: %s", inv)
	}

	exported := k.ExportGenesis(ctx)
	require.NoError(t, exported.Validate(), "ExportGenesis must never emit a state its own Validate rejects")

	importCtx, importK := freshIdentityKeeper(t)
	require.NotPanics(t, func() { importK.InitGenesis(importCtx, *exported) })

	require.Equal(t, exported, importK.ExportGenesis(importCtx))
	if inv, broken := keeper.AllInvariants(importK)(importCtx); broken {
		t.Fatalf("imported state breaks an identity invariant: %s", inv)
	}
}

// TestGenesis_SetGuardiansThenRevoke_RoundTrips is the exact two-transaction sequence that used to poison a chain permanently: both messages are permissionless and are sent by the DID's own controller, yet together they produced state that exported cleanly and then panicked InitGenesis on the next restart, with no transaction available to repair it.
func TestGenesis_SetGuardiansThenRevoke_RoundTrips(t *testing.T) {
	ctx, k, msg, _ := setupIdentityFull(t, phicryptoAcceptAll())
	ctx = ctx.WithBlockTime(time.Unix(1_000_000, 0))

	ctrl := someAddr("poison-owner________")
	did := registerActive(t, ctx, msg, ctrl, "poison-owner", []byte("bio-poison"))
	_, commitments := guardianPool(t, ctx, msg, 3)

	_, err := msg.SetGuardians(ctx, &types.MsgSetGuardians{
		Controller: ctrl, Did: did, Commitments: commitments, Threshold: 2,
	})
	require.NoError(t, err)
	_, err = msg.RevokeIdentity(ctx, &types.MsgRevokeIdentity{Creator: ctrl, Did: did})
	require.NoError(t, err)

	exported := k.ExportGenesis(ctx)
	require.NoError(t, exported.Validate())
	for _, g := range exported.GuardianSets {
		require.NotEqual(t, did, g.Did, "a revoked DID's guardian set must not be exported")
	}

	importCtx, importK := freshIdentityKeeper(t)
	require.NotPanics(t, func() { importK.InitGenesis(importCtx, *exported) })
	require.Equal(t, exported, importK.ExportGenesis(importCtx))
}
