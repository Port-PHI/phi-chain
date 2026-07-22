// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	insttypes "github.com/Port-PHI/phi-chain/x/institutions/types"

	identitykeeper "github.com/Port-PHI/phi-chain/x/identity/keeper"
	identitytypes "github.com/Port-PHI/phi-chain/x/identity/types"
)

type failingStaking struct {
	inner  identitykeeper.ValidatorSweepStaking
	target sdk.ConsAddress
	panics bool
	calls  *int
}

func (f failingStaking) IterateLastValidators(ctx context.Context, fn func(int64, stakingtypes.ValidatorI) bool) error {
	return f.inner.IterateLastValidators(ctx, fn)
}

func (f failingStaking) Jail(ctx context.Context, consAddr sdk.ConsAddress) error {
	if consAddr.Equals(f.target) {
		*f.calls++
		if f.panics {
			panic(fmt.Sprintf("validator with consensus-address %s not found", consAddr))
		}
		return errors.New("injected staking failure")
	}
	return f.inner.Jail(ctx, consAddr)
}

type failingIteration struct {
	identitykeeper.ValidatorSweepStaking
}

func (failingIteration) IterateLastValidators(context.Context, func(int64, stakingtypes.ValidatorI) bool) error {
	return errors.New("injected iteration failure")
}

// A failure for one validator must not halt the block: no error, no panic, everyone else judged, the failing one untouched.
func TestSweepFailSafe_AFailingValidatorDoesNotHaltTheBlock(t *testing.T) {
	for _, mode := range []struct {
		name   string
		panics bool
	}{
		{"jail returns an error", false},
		{"jail panics", true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			victim := newSweepFixture(t, "failsafe-victim")
			victim.registerDID(t, identitytypes.DID_STATUS_SUSPENDED, true)

			bystander := newSweepFixtureOn(t, victim, "failsafe-bystander")
			bystander.registerDID(t, identitytypes.DID_STATUS_SUSPENDED, true)

			calls := 0
			sk := failingStaking{
				inner: victim.a.StakingKeeper, target: victim.consAddr,
				panics: mode.panics, calls: &calls,
			}

			var outcomes map[string]identitykeeper.ValidatorSweepOutcome
			var err error
			require.NotPanics(t, func() {
				outcomes, err = victim.a.IdentityKeeper.SweepValidatorBindings(
					victim.ctx, sk, victim.a.SlashingKeeper)
			}, "a failing validator must never panic out of the sweep")
			require.NoError(t, err,
				"an error here aborts the block, deterministically, on every node and every retry")
			require.Positive(t, calls, "the injected failure must actually have been reached")

			require.Equal(t, identitykeeper.SweepJailed, outcomes[bystander.valAddr.String()],
				"one validator's failure must not cost the others their sweep")
			require.False(t, bystander.inActiveSet(t))

			_, reported := outcomes[victim.valAddr.String()]
			require.False(t, reported, "a skipped validator must not be reported as judged")
			require.True(t, victim.inActiveSet(t),
				"the failed action must leave no partial state behind")
			require.False(t, victim.tombstoned())
		})
	}
}

// Skipping is only acceptable because the sweep runs again: the skipped validator is retried next block.
func TestSweepFailSafe_TheSkippedValidatorIsRetriedNextBlock(t *testing.T) {
	f := newSweepFixture(t, "failsafe-retry")
	f.registerDID(t, identitytypes.DID_STATUS_SUSPENDED, true)

	calls := 0
	sk := failingStaking{inner: f.a.StakingKeeper, target: f.consAddr, panics: true, calls: &calls}

	require.NotPanics(t, func() {
		_, err := f.a.IdentityKeeper.SweepValidatorBindings(f.ctx, sk, f.a.SlashingKeeper)
		require.NoError(t, err)
	})
	require.True(t, f.inActiveSet(t), "still validating, because nothing was applied")

	outcomes := f.sweep(t)
	require.Equal(t, identitykeeper.SweepJailed, outcomes[f.valAddr.String()])
	require.False(t, f.inActiveSet(t), "the retry is what makes skipping safe")
}

// A failure of the validator walk itself must still not stop the block.
func TestSweepFailSafe_AFailingIterationDoesNotHaltTheBlock(t *testing.T) {
	f := newSweepFixture(t, "failsafe-iter")
	f.registerDID(t, identitytypes.DID_STATUS_REVOKED, true)

	require.NotPanics(t, func() {
		outcomes, err := f.a.IdentityKeeper.SweepValidatorBindings(
			f.ctx, failingIteration{f.a.StakingKeeper}, f.a.SlashingKeeper)
		require.NoError(t, err, "an unreadable validator set must not abort the block")
		require.Empty(t, outcomes)
	})
	require.True(t, f.inActiveSet(t), "nothing was judged, so nothing changed")
}

func newSweepFixtureOn(t *testing.T, base *sweepFixture, label string) *sweepFixture {
	t.Helper()
	acct := sdk.AccAddress([]byte(fmt.Sprintf("%-20s", label)[:20]))
	valAddr := sdk.ValAddress(acct)

	phi := math.NewIntFromUint64(cointypes.UphiPerPhi)
	stake := phi.MulRaw(100)
	require.NoError(t, base.a.BankKeeper.MintCoins(base.ctx, insttypes.ModuleName, cointypes.CoinsOf(stake)))
	require.NoError(t, base.a.BankKeeper.SendCoinsFromModuleToAccount(
		base.ctx, insttypes.ModuleName, acct, cointypes.CoinsOf(stake)))

	consPub := ed25519.GenPrivKeyFromSecret([]byte(label + "-cons")).PubKey()
	val, err := stakingtypes.NewValidator(valAddr.String(), consPub, stakingtypes.Description{Moniker: label})
	require.NoError(t, err)
	val.Status = stakingtypes.Unbonded
	require.NoError(t, base.a.StakingKeeper.SetValidator(base.ctx, val))
	require.NoError(t, base.a.StakingKeeper.SetValidatorByConsAddr(base.ctx, val))
	require.NoError(t, base.a.StakingKeeper.SetNewValidatorByPowerIndex(base.ctx, val))
	require.NoError(t, base.a.DistrKeeper.Hooks().AfterValidatorCreated(base.ctx, valAddr))
	_, err = base.a.StakingKeeper.Delegate(base.ctx, acct, stake, stakingtypes.Unbonded, val, true)
	require.NoError(t, err)
	_, err = base.a.StakingKeeper.EndBlocker(base.ctx)
	require.NoError(t, err)

	consAddr := sdk.ConsAddress(consPub.Address())
	require.NoError(t, base.a.SlashingKeeper.AddPubkey(base.ctx, consPub))
	require.NoError(t, base.a.SlashingKeeper.SetValidatorSigningInfo(base.ctx, consAddr,
		slashingtypes.NewValidatorSigningInfo(consAddr, base.ctx.BlockHeight(), 0, time.Time{}, false, 0)))

	return &sweepFixture{a: base.a, ctx: base.ctx, valAddr: valAddr, consAddr: consAddr, acct: acct}
}
