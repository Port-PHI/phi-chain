// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"testing"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	identitytypes "github.com/Port-PHI/phi-chain/x/identity/types"
)

type roundTripCase struct {
	name   string
	hasDID bool
	status identitytypes.DIDStatus
	bound  bool
}

func roundTripCases() []roundTripCase {
	var cases []roundTripCase
	for _, bound := range []bool{false, true} {
		for _, st := range []struct {
			name   string
			hasDID bool
			status identitytypes.DIDStatus
		}{
			{"active", true, identitytypes.DID_STATUS_ACTIVE},
			{"suspended", true, identitytypes.DID_STATUS_SUSPENDED},
			{"revoked", true, identitytypes.DID_STATUS_REVOKED},
			{"no-did", false, identitytypes.DID_STATUS_UNSPECIFIED},
		} {
			label := "unbound/" + st.name
			if bound {
				label = "bound/" + st.name
			}
			cases = append(cases, roundTripCase{
				name: label, hasDID: st.hasDID, status: st.status, bound: bound,
			})
		}
	}
	return cases
}

// Every cell is driven to its runtime state, swept, and required to survive the genesis round-trip unchanged.
func TestNet_ValidatorBindingRoundTripsThroughGenesis(t *testing.T) {
	for _, tc := range roundTripCases() {
		t.Run(tc.name, func(t *testing.T) {
			f := newSweepFixture(t, "rt-"+tc.name)
			if tc.hasDID {
				f.registerDID(t, tc.status, tc.bound)
				f.a.IdentityKeeper.SetIdentityCount(f.ctx, 1)
			} else if tc.bound {
				f.a.IdentityKeeper.BindValidatorToDID(f.ctx, "did:phi:absent", f.valAddr.String())
			}

			f.sweep(t)

			exported := f.a.IdentityKeeper.ExportGenesis(f.ctx)
			require.NoError(t, exported.Validate(),
				"the runtime produced a state that genesis validation rejects: it can be exported but "+
					"never imported again")

			b := newTestApp(t)
			ctxB := b.NewUncachedContext(false, cmtproto.Header{Height: 1, Time: f.ctx.BlockTime()})
			require.NotPanics(t, func() {
				b.IdentityKeeper.InitGenesis(ctxB, *exported)
			}, "a state the runtime produced must import without panicking")

			require.Equal(t, exported, b.IdentityKeeper.ExportGenesis(ctxB),
				"export → import → export must be the identity function")
		})
	}
}

// Genesis validation must reject a binding to a REVOKED DID (a shape the runtime never leaves behind).
func TestNet_GenesisRejectsABindingToARevokedDID(t *testing.T) {
	f := newSweepFixture(t, "rt-reject")
	f.registerDID(t, identitytypes.DID_STATUS_REVOKED, true)

	require.Error(t, f.a.IdentityKeeper.ExportGenesis(f.ctx).Validate(),
		"a binding to a revoked identity must never validate as genesis")
}
