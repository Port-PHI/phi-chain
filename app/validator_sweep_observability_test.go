// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	identitykeeper "github.com/Port-PHI/phi-chain/x/identity/keeper"
	identitytypes "github.com/Port-PHI/phi-chain/x/identity/types"
)

func sweepFailedEvents(ctx sdk.Context, valoper string) []string {
	var reasons []string
	for _, e := range ctx.EventManager().Events() {
		if e.Type != identitytypes.EventTypeValidatorSweepFailed {
			continue
		}
		attrs := map[string]string{}
		for _, a := range e.Attributes {
			attrs[a.Key] = a.Value
		}
		if attrs[identitytypes.AttributeKeyValidator] == valoper {
			reasons = append(reasons, attrs[identitytypes.AttributeKeyReason])
		}
	}
	return reasons
}

// TestSweepObservability_APersistentFailureIsAnnounced covers both shapes the failure takes: an error out of staking, and the panic staking's own must-get raises on a consensus address it cannot resolve.
func TestSweepObservability_APersistentFailureIsAnnounced(t *testing.T) {
	for _, mode := range []struct {
		name   string
		panics bool
	}{
		{"jail returns an error", false},
		{"jail panics", true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			f := newSweepFixture(t, "observe-"+mode.name[:5])
			f.registerDID(t, identitytypes.DID_STATUS_REVOKED, true)

			calls := 0
			sk := failingStaking{
				inner: f.a.StakingKeeper, target: f.consAddr, panics: mode.panics, calls: &calls,
			}

			require.NotPanics(t, func() {
				_, err := f.a.IdentityKeeper.SweepValidatorBindings(f.ctx, sk, f.a.SlashingKeeper)
				require.NoError(t, err)
			})
			require.Positive(t, calls, "the injected failure must actually have been reached")

			require.True(t, f.inActiveSet(t))

			reasons := sweepFailedEvents(f.ctx, f.valAddr.String())
			require.Len(t, reasons, 1,
				"a validator the sweep could not act on must be announced, not only logged")
			require.NotEmpty(t, reasons[0], "the event must carry why the sweep could not act")
		})
	}
}

// A sweep that succeeds must stay quiet.
func TestSweepObservability_ASuccessfulSweepAnnouncesNoFailure(t *testing.T) {
	f := newSweepFixture(t, "observe-ok")
	f.registerDID(t, identitytypes.DID_STATUS_REVOKED, true)

	f.sweep(t)
	require.False(t, f.inActiveSet(t), "precondition: the sweep did act")
	require.Empty(t, sweepFailedEvents(f.ctx, f.valAddr.String()))
}

// TestSweepObservability_OrderingCannotEvadeTheSweepDecision is the comprehensive test: the sweep's ACTIVE-vs-not decision reads an O(1) per-controller record, so it can no longer be evaded by the order of an operator's DIDs.
func TestSweepObservability_OrderingCannotEvadeTheSweepDecision(t *testing.T) {
	const past = identitytypes.MaxControllerDIDScan * 3

	f := newSweepFixture(t, "sweep-hidden-active")
	for i := 0; i < past; i++ {
		f.a.IdentityKeeper.SetIdentity(f.ctx, identitytypes.DIDDocument{
			Did:            fmt.Sprintf("did:phi:aaa-%04d-%s", i, f.acct.String()), // sort BEFORE the active one
			Controller:     f.acct.String(),
			PubKey:         []byte("pk"),
			UniquenessHash: []byte(fmt.Sprintf("uniq-aaa-%04d-%s", i, f.acct.String())),
			Status:         identitytypes.DID_STATUS_REVOKED,
			CreatedAt:      f.ctx.BlockTime().Unix() - 1_000_000,
		})
	}
	hiddenActive := fmt.Sprintf("did:phi:zzz-active-%s", f.acct.String()) // sorts AFTER every revoked one
	f.a.IdentityKeeper.SetIdentity(f.ctx, identitytypes.DIDDocument{
		Did:            hiddenActive,
		Controller:     f.acct.String(),
		PubKey:         []byte("pk"),
		UniquenessHash: []byte("uniq-" + hiddenActive),
		Status:         identitytypes.DID_STATUS_ACTIVE,
		CreatedAt:      f.ctx.BlockTime().Unix() - 1_000_000,
	})

	outcomes := f.sweep(t)
	require.Equal(t, identitykeeper.SweepBound, outcomes[f.valAddr.String()],
		"an ACTIVE DID hidden past the old scan bound must still be seen as ACTIVE")
	require.True(t, f.inActiveSet(t), "the operator holds an ACTIVE DID, so it is kept")
	bound, ok := f.a.IdentityKeeper.DIDForValidator(f.ctx, f.valAddr.String())
	require.True(t, ok)
	require.Equal(t, hiddenActive, bound, "the sweep bound the hidden ACTIVE DID, whatever its key order")
	require.Empty(t, sweepFailedEvents(f.ctx, f.valAddr.String()), "no skip: the decision is O(1), never truncated")

	g := newSweepFixture(t, "sweep-all-revoked")
	for i := 0; i < past+1; i++ {
		g.a.IdentityKeeper.SetIdentity(g.ctx, identitytypes.DIDDocument{
			Did:            fmt.Sprintf("did:phi:rev-%04d-%s", i, g.acct.String()),
			Controller:     g.acct.String(),
			PubKey:         []byte("pk"),
			UniquenessHash: []byte(fmt.Sprintf("uniq-rev-%04d-%s", i, g.acct.String())),
			Status:         identitytypes.DID_STATUS_REVOKED,
			CreatedAt:      g.ctx.BlockTime().Unix() - 1_000_000,
		})
	}
	gout := g.sweep(t)
	require.Equal(t, identitykeeper.SweepTombstoned, gout[g.valAddr.String()],
		"an operator whose every DID is REVOKED is terminally gone: tombstoned, not skipped")
	require.False(t, g.inActiveSet(t))
	require.True(t, g.tombstoned())
	require.Empty(t, sweepFailedEvents(g.ctx, g.valAddr.String()), "no skip: the decision is O(1), never truncated")
}
