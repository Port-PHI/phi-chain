// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

var bindingValoper = sdk.ValAddress([]byte("binding-lifecycle-op")).String()

// TestBindingLifecycle_RevocationReleasesTheBinding is the sequence that used to poison a chain: two permissionless transactions from the identity's own controller, leaving behind a binding no genesis can express.
func TestBindingLifecycle_RevocationReleasesTheBinding(t *testing.T) {
	ctx, k, msg, _ := setupIdentityFull(t, phicryptoAcceptAll())
	ctx = ctx.WithBlockTime(time.Unix(1_000_000, 0))

	ctrl := someAddr("binding-owner_______")
	did := registerActive(t, ctx, msg, ctrl, "binding-owner", []byte("bio-binding"))
	k.BindValidatorToDID(ctx, did, bindingValoper)

	require.NoError(t, k.ExportGenesis(ctx).Validate(), "a binding to an ACTIVE DID is valid genesis")

	_, err := msg.RevokeIdentity(ctx, &types.MsgRevokeIdentity{Creator: ctrl, Did: did})
	require.NoError(t, err)

	_, stillBound := k.ValidatorForDID(ctx, did)
	require.False(t, stillBound, "revocation must release the validator binding")
	_, stillReverse := k.DIDForValidator(ctx, bindingValoper)
	require.False(t, stillReverse, "both directions of the binding must go")

	exported := k.ExportGenesis(ctx)
	require.NoError(t, exported.Validate(),
		"a revoked identity must not leave behind a binding that genesis refuses")

	importCtx, importK := freshIdentityKeeper(t)
	require.NotPanics(t, func() { importK.InitGenesis(importCtx, *exported) })
	require.Equal(t, exported, importK.ExportGenesis(importCtx))
}

// Suspension is reversible, so the binding stays — and genesis has to accept it, or a routine suspension would make the chain unrestartable.
func TestBindingLifecycle_SuspensionKeepsTheBinding(t *testing.T) {
	ctx, k, msg, _ := setupIdentityFull(t, phicryptoAcceptAll())
	ctx = ctx.WithBlockTime(time.Unix(1_000_000, 0))

	ctrl := someAddr("suspend-owner_______")
	did := registerActive(t, ctx, msg, ctrl, "suspend-owner", []byte("bio-suspend"))
	k.BindValidatorToDID(ctx, did, bindingValoper)

	_, err := msg.UpdateStatus(ctx, &types.MsgUpdateStatus{
		Authority: k.GetAuthority(), Did: did, NewStatus: types.DID_STATUS_SUSPENDED,
	})
	require.NoError(t, err)

	bound, ok := k.ValidatorForDID(ctx, did)
	require.True(t, ok, "a reversible freeze must not strand the operator")
	require.Equal(t, bindingValoper, bound)

	exported := k.ExportGenesis(ctx)
	require.NoError(t, exported.Validate(),
		"a suspended identity's binding is a state the runtime maintains and genesis must accept")

	importCtx, importK := freshIdentityKeeper(t)
	require.NotPanics(t, func() { importK.InitGenesis(importCtx, *exported) })
	require.Equal(t, exported, importK.ExportGenesis(importCtx))

	reimported, ok := importK.ValidatorForDID(importCtx, did)
	require.True(t, ok)
	require.Equal(t, bindingValoper, reimported)
}
