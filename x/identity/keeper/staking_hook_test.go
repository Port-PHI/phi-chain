// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/keeper"
	"github.com/Port-PHI/phi-chain/x/identity/types"
)

type fakeStaking struct {
	minSelf math.Int
	tokens  math.Int
}

func (f fakeStaking) GetValidator(_ context.Context, _ sdk.ValAddress) (stakingtypes.Validator, error) {
	return stakingtypes.Validator{MinSelfDelegation: f.minSelf, Tokens: f.tokens}, nil
}

var floor = math.NewInt(1000)

func TestValidatorHook_RequiresDIDAndMinStake(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	ctx = ctx.WithBlockHeight(1) // runtime (not genesis)

	accBytes := []byte("validator1__________")
	controller := sdk.AccAddress(accBytes).String()
	valAddr := sdk.ValAddress(accBytes)

	hooks := k.NewValidatorHooks(fakeStaking{minSelf: floor}, floor)
	err := hooks.AfterValidatorCreated(ctx, valAddr)
	require.ErrorIs(t, err, types.ErrValidatorNeedsDID)

	_, err = msg.RegisterIdentity(ctx, reg(controller, "v1", []byte("bio-v1")))
	require.NoError(t, err)

	require.NoError(t, hooks.AfterValidatorCreated(ctx, valAddr))
	bound, ok := k.ValidatorForDID(ctx, didFor("v1"))
	require.True(t, ok)
	require.Equal(t, valAddr.String(), bound)
}

func TestValidatorHook_RejectsLowSelfStake(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	ctx = ctx.WithBlockHeight(1)

	accBytes := []byte("validator2__________")
	controller := sdk.AccAddress(accBytes).String()
	valAddr := sdk.ValAddress(accBytes)
	_, err := msg.RegisterIdentity(ctx, reg(controller, "v2", []byte("bio-v2")))
	require.NoError(t, err)

	low := k.NewValidatorHooks(fakeStaking{minSelf: math.NewInt(999)}, floor)
	require.ErrorIs(t, low.AfterValidatorCreated(ctx, valAddr), types.ErrMinSelfDelegation)
}

func TestValidatorHook_RejectsDuplicateDID(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	ctx = ctx.WithBlockHeight(1)

	accBytes := []byte("validator3__________")
	controller := sdk.AccAddress(accBytes).String()
	valAddr := sdk.ValAddress(accBytes)
	_, err := msg.RegisterIdentity(ctx, reg(controller, "v3", []byte("bio-v3")))
	require.NoError(t, err)

	k.BindValidatorToDID(ctx, didFor("v3"), "phivaloperOTHER")
	hooks := k.NewValidatorHooks(fakeStaking{minSelf: floor}, floor)
	require.ErrorIs(t, hooks.AfterValidatorCreated(ctx, valAddr), types.ErrDIDAlreadyValidator)
}

func TestValidatorHook_GenesisBypass(t *testing.T) {
	ctx, k, _ := setupIdentity(t)
	ctx = ctx.WithBlockHeight(0) // genesis / InitChain

	valAddr := sdk.ValAddress([]byte("genesis_validator___"))
	hooks := k.NewValidatorHooks(fakeStaking{minSelf: math.NewInt(0)}, floor)
	require.NoError(t, hooks.AfterValidatorCreated(ctx, valAddr))
}

func TestValidatorHook_RemovedUnbinds(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	ctx = ctx.WithBlockHeight(1)

	accBytes := []byte("validator5__________")
	controller := sdk.AccAddress(accBytes).String()
	valAddr := sdk.ValAddress(accBytes)
	_, err := msg.RegisterIdentity(ctx, reg(controller, "v5", []byte("bio-v5")))
	require.NoError(t, err)

	hooks := k.NewValidatorHooks(fakeStaking{minSelf: floor}, floor)
	require.NoError(t, hooks.AfterValidatorCreated(ctx, valAddr))
	_, ok := k.ValidatorForDID(ctx, didFor("v5"))
	require.True(t, ok)

	require.NoError(t, hooks.AfterValidatorRemoved(ctx, sdk.ConsAddress(accBytes), valAddr))
	_, ok = k.ValidatorForDID(ctx, didFor("v5"))
	require.False(t, ok)
}

func seedValidatorGateState(t *testing.T, k keeper.Keeper, ctx sdk.Context, decoys int) (sdk.ValAddress, string) {
	t.Helper()

	accBytes := []byte("validator_index_____")
	controller := sdk.AccAddress(accBytes).String()

	for i := 0; i < decoys; i++ {
		k.SetIdentity(ctx, types.DIDDocument{
			Did:        fmt.Sprintf("did:phi:aaa-decoy-%05d", i),
			Controller: sdk.AccAddress([]byte(fmt.Sprintf("decoy-controller-%03d", i))).String(),
			Status:     types.DID_STATUS_ACTIVE,
			CreatedAt:  100,
			PubKey:     []byte("decoy"),
		})
	}

	k.SetIdentity(ctx, types.DIDDocument{
		Did: "did:phi:mmm-target-revoked", Controller: controller,
		Status: types.DID_STATUS_REVOKED, CreatedAt: 100, PubKey: []byte("old"),
	})
	activeDID := "did:phi:nnn-target-active"
	k.SetIdentity(ctx, types.DIDDocument{
		Did: activeDID, Controller: controller,
		Status: types.DID_STATUS_ACTIVE, CreatedAt: 100, PubKey: []byte("cur"),
	})

	return sdk.ValAddress(accBytes), activeDID
}

// The validator gate resolves the controller's DID through the bounded (controller ‖ did) index.
func TestValidatorHook_ResolvesDIDThroughBoundedIndex(t *testing.T) {
	run := func(decoys int) (uint64, string) {
		ctx, k, _ := setupIdentity(t)
		ctx = ctx.WithBlockHeight(1) // runtime (not genesis)
		valAddr, activeDID := seedValidatorGateState(t, k, ctx, decoys)

		hooks := k.NewValidatorHooks(fakeStaking{minSelf: floor}, floor)
		ctx = ctx.WithGasMeter(storetypes.NewGasMeter(100_000_000))
		before := ctx.GasMeter().GasConsumed()
		require.NoError(t, hooks.AfterValidatorCreated(ctx, valAddr))
		used := ctx.GasMeter().GasConsumed() - before

		bound, ok := k.ValidatorForDID(ctx, activeDID)
		require.True(t, ok, "the ACTIVE DID must be bound to the validator")
		require.Equal(t, valAddr.String(), bound)
		_, revokedBound := k.ValidatorForDID(ctx, "did:phi:mmm-target-revoked")
		require.False(t, revokedBound, "a REVOKED DID must never back a validator")

		did, ok := k.DIDForValidator(ctx, valAddr.String())
		require.True(t, ok)
		return used, did
	}

	smallGas, smallDID := run(10)
	largeGas, largeDID := run(400)

	require.Equal(t, smallDID, largeDID, "the resolved DID must not depend on registry size")

	require.Equal(t, smallGas, largeGas,
		"validator-creation gas must be independent of the number of identities in the registry")
}

// BeforeValidatorSlashed is a no-op: supply conservation across a slash is handled by the app-level slash-compensation wrapper (measured whole-slash delta), not by this hook, because the hook fires after the SDK has already burned unbonding/redelegation balances (see TestSlashCompensation_* in the app package for the end-to-end regression).
func TestValidatorHook_SlashedIsNoop(t *testing.T) {
	ctx, k, _ := setupIdentity(t)
	ctx = ctx.WithBlockHeight(5)
	valAddr := sdk.ValAddress([]byte("validator6__________"))

	hooks := k.NewValidatorHooks(fakeStaking{minSelf: floor, tokens: math.NewInt(1_000_000)}, floor)
	require.NoError(t, hooks.BeforeValidatorSlashed(ctx, valAddr, math.LegacyNewDecWithPrec(5, 2)))
}
