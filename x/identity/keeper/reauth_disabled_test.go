//go:build !reauth

// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/keeper"
	"github.com/Port-PHI/phi-chain/x/identity/types"
)

func TestReauth_DisabledByDefault(t *testing.T) {
	require.False(t, keeper.ReauthRecoveryEnabled,
		"REAUTH must be OFF in the default build: it is enabled only by a maintainer rebuild with -tags reauth")
}

// The message is rejected with ErrReauthNotEnabled — and nothing at all happens: no request is written, no nonce is consumed, no fee or deposit moves.
func TestReauth_InitiateRejectedFailClosed(t *testing.T) {
	f := setupRecovery(t)
	totalBefore := f.bank.Total()
	balanceBefore := f.newCtrlBalance(t)

	m := f.initiateMsg(recoveryNonce, f.newKey)
	m.Method = types.RECOVERY_METHOD_REAUTH
	m.AttestorDid = testIssuerDID
	m.ReauthAttestation = []byte("an attestation this binary will never even look at")

	_, err := f.msg.InitiateRecovery(f.ctx, m)
	require.ErrorIs(t, err, types.ErrReauthNotEnabled)

	require.Empty(t, f.k.RecoveryRequestsForDID(f.ctx, f.did), "no request was opened")
	require.Equal(t, balanceBefore, f.newCtrlBalance(t), "the initiator was not charged")
	require.True(t, f.escrowed().IsZero())
	require.True(t, f.feeCollector().IsZero())
	require.Equal(t, totalBefore, f.bank.Total())

	require.NotEmpty(t, f.initiate(t, recoveryNonce), "the nonce was NOT consumed by the rejected message")
}

// The SOCIAL path is untouched by the gate (no Slice 0–4 regression): the identical request, with method=SOCIAL, still opens.
func TestReauth_GateDoesNotAffectSocial(t *testing.T) {
	f := setupRecovery(t)
	id := f.initiate(t, recoveryNonce)
	require.Equal(t, types.RECOVERY_STATUS_PENDING, f.status(t, id))
}
