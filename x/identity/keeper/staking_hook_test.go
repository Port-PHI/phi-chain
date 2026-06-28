// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

// fakeStaking is a fake validator source with a configurable min_self_delegation and token balance.
type fakeStaking struct {
	minSelf math.Int
	tokens  math.Int
}

func (f fakeStaking) GetValidator(_ context.Context, _ sdk.ValAddress) (stakingtypes.Validator, error) {
	return stakingtypes.Validator{MinSelfDelegation: f.minSelf, Tokens: f.tokens}, nil
}

// floor is the test self-stake floor = 1000 (symbolic unit).
var floor = math.NewInt(1000)

func TestValidatorHook_RequiresDIDAndMinStake(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	ctx = ctx.WithBlockHeight(1) // runtime (not genesis)

	accBytes := []byte("validator1__________")
	controller := sdk.AccAddress(accBytes).String()
	valAddr := sdk.ValAddress(accBytes)

	// Without a DID: rejected.
	hooks := k.NewValidatorHooks(fakeStaking{minSelf: floor}, floor)
	err := hooks.AfterValidatorCreated(ctx, valAddr)
	require.ErrorIs(t, err, types.ErrValidatorNeedsDID)

	// Register an active DID for the same account.
	_, err = msg.RegisterIdentity(ctx, reg(controller, "v1", []byte("bio-v1")))
	require.NoError(t, err)

	// With a DID and sufficient stake: succeeds and the DID-to-validator binding is recorded.
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

	// min_self_delegation below the floor: rejected.
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

	// The same DID is already bound to another validator: rejected.
	k.BindValidatorToDID(ctx, didFor("v3"), "phivaloperOTHER")
	hooks := k.NewValidatorHooks(fakeStaking{minSelf: floor}, floor)
	require.ErrorIs(t, hooks.AfterValidatorCreated(ctx, valAddr), types.ErrDIDAlreadyValidator)
}

func TestValidatorHook_GenesisBypass(t *testing.T) {
	ctx, k, _ := setupIdentity(t)
	ctx = ctx.WithBlockHeight(0) // genesis / InitChain

	valAddr := sdk.ValAddress([]byte("genesis_validator___"))
	hooks := k.NewValidatorHooks(fakeStaking{minSelf: math.NewInt(0)}, floor)
	// At genesis no gate is applied (neither DID nor stake floor).
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

	// Removing the validator releases the binding (DID reusable).
	require.NoError(t, hooks.AfterValidatorRemoved(ctx, sdk.ConsAddress(accBytes), valAddr))
	_, ok = k.ValidatorForDID(ctx, didFor("v5"))
	require.False(t, ok)
}

// BeforeValidatorSlashed is a no-op: supply conservation across a slash is handled by the app-level
// slash-compensation wrapper (measured whole-slash delta), not by this hook, because the hook fires
// after the SDK has already burned unbonding/redelegation balances (see TestSlashCompensation_* in
// the app package for the end-to-end regression).
func TestValidatorHook_SlashedIsNoop(t *testing.T) {
	ctx, k, _ := setupIdentity(t)
	ctx = ctx.WithBlockHeight(5)
	valAddr := sdk.ValAddress([]byte("validator6__________"))

	hooks := k.NewValidatorHooks(fakeStaking{minSelf: floor, tokens: math.NewInt(1_000_000)}, floor)
	require.NoError(t, hooks.BeforeValidatorSlashed(ctx, valAddr, math.LegacyNewDecWithPrec(5, 2)))
}
