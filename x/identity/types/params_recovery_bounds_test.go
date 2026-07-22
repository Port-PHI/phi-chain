// SPDX-License-Identifier: Apache-2.0

package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The per-DID open-request cap is not merely a policy knob: the open-slot count at initiate and the sibling supersede at execute both iterate up to that many requests, inside gas-metered handlers.
func TestParams_MaxOpenRecoveryRequestsIsBounded(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   uint32
		wantErr bool
	}{
		{"zero is rejected: no recovery could ever open", 0, true},
		{"one is accepted", 1, false},
		{"the shipped default is accepted", DefaultMaxOpenRecoveryRequests, false},
		{"the ceiling itself is accepted", MaxOpenRecoveryRequestsCeiling, false},
		{"one above the ceiling is rejected", MaxOpenRecoveryRequestsCeiling + 1, true},
		{"an absurd value is rejected", 1 << 20, true},
		{"the maximum representable value is rejected", ^uint32(0), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := DefaultParams()
			p.MaxOpenRecoveryRequests = tc.value
			err := p.Validate()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// The cap is bounded the same way the guardian-set size is, and for the same reason — both size work the chain performs on behalf of one DID.
func TestParams_RecoveryAndGuardianCapsAreBothBounded(t *testing.T) {
	overGuardians := DefaultParams()
	overGuardians.MaxGuardians = 0
	require.Error(t, overGuardians.Validate())

	overOpen := DefaultParams()
	overOpen.MaxOpenRecoveryRequests = MaxOpenRecoveryRequestsCeiling + 1
	require.Error(t, overOpen.Validate())

	require.NoError(t, DefaultParams().Validate(), "the shipped defaults must satisfy both bounds")
}
