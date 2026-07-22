// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	identitykeeper "github.com/Port-PHI/phi-chain/x/identity/keeper"
	identitytypes "github.com/Port-PHI/phi-chain/x/identity/types"
)

// TestGenesisFounders_MissingDIDFailsLoudly is the first guard: a validator that is validating without an ACTIVE DID stops the chain at startup, with an error naming it.
func TestGenesisFounders_MissingDIDFailsLoudly(t *testing.T) {
	f := newSweepFixture(t, "founder-missing")
	require.True(t, f.inActiveSet(t), "precondition: the founder is validating")

	err := f.a.IdentityKeeper.ValidateGenesisFounders(f.ctx, f.a.StakingKeeper)
	require.Error(t, err, "a validating founder with no ACTIVE DID must fail genesis")
	require.Contains(t, err.Error(), f.valAddr.String(),
		"the error must name the offending operator, or it cannot be acted on")
	require.Contains(t, err.Error(), "ACTIVE DID")
}

// With the DID present the same genesis passes, so the check gates on the real condition rather than simply refusing every validator.
func TestGenesisFounders_PresentDIDPasses(t *testing.T) {
	f := newSweepFixture(t, "founder-present")
	f.registerDID(t, identitytypes.DID_STATUS_ACTIVE, false)

	require.NoError(t, f.a.IdentityKeeper.ValidateGenesisFounders(f.ctx, f.a.StakingKeeper),
		"a founder whose account holds an ACTIVE DID is exactly what the sweep will adopt")
}

// The check must not make a legitimately exported chain unrestartable.
func TestGenesisFounders_AlreadyJailedValidatorsDoNotBlockImport(t *testing.T) {
	f := newSweepFixture(t, "founder-jailed")

	require.Equal(t, identitykeeper.SweepJailed, f.sweep(t)[f.valAddr.String()])
	require.False(t, f.inActiveSet(t))
	require.False(t, f.tombstoned(), "a recoverable operator must never be tombstoned")

	require.NoError(t, f.a.IdentityKeeper.ValidateGenesisFounders(f.ctx, f.a.StakingKeeper),
		"an already-removed validator is live state, and refusing it would make the chain unrestartable")
}

// The second guard: a founder the first guard missed is only JAILED, so registering the DID and unjailing restores it.
func TestGenesisFounders_AJailedFounderRecoversAfterRegisteringTheDID(t *testing.T) {
	f := newSweepFixture(t, "founder-recover")

	require.Equal(t, identitykeeper.SweepJailed, f.sweep(t)[f.valAddr.String()])
	require.False(t, f.inActiveSet(t))
	require.False(t, f.tombstoned(), "the founder set must stay recoverable")

	f.registerDID(t, identitytypes.DID_STATUS_ACTIVE, false)

	require.NoError(t, f.a.SlashingKeeper.Unjail(f.ctx, f.valAddr))

	require.Equal(t, identitykeeper.SweepBound, f.sweep(t)[f.valAddr.String()])
	require.True(t, f.inActiveSet(t), "the founder is validating again")

	bound, ok := f.a.IdentityKeeper.DIDForValidator(f.ctx, f.valAddr.String())
	require.True(t, ok, "and is now bound like any other validator")
	require.Equal(t, f.did, bound)
}
