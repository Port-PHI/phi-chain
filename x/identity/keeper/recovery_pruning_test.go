// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"fmt"
	"testing"

	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

func (f *recoveryFixture) fundForRounds(t *testing.T, rounds int) {
	t.Helper()
	addr, err := sdk.AccAddressFromBech32(f.newCtrl)
	require.NoError(t, err)
	f.bank.Fund(addr, f.deposit.MulRaw(int64(rounds)+10))
}

func (f *recoveryFixture) initiateAndReject(t *testing.T, round int) {
	t.Helper()
	res, err := f.msg.InitiateRecovery(f.ctx,
		f.initiateMsg(fmt.Sprintf("grief-nonce-%03d", round), pubFor(fmt.Sprintf("grief-key-%03d", round))))
	require.NoError(t, err)
	f.rejectToThreshold(t, res.RecoveryId)
	f.requireSettled(t, res.RecoveryId)
}

// The pin: after many settle cycles the per-DID index scan costs exactly what it did after a few.
func TestRecovery_SettledRequestsDoNotGrowTheIndexScan(t *testing.T) {
	measure := func(rounds int) uint64 {
		f := setupRecovery(t)
		f.fundForRounds(t, rounds)
		for i := 0; i < rounds; i++ {
			f.initiateAndReject(t, i)
		}

		f.ctx = f.ctx.WithGasMeter(storetypes.NewGasMeter(500_000_000))
		before := f.ctx.GasMeter().GasConsumed()
		got := f.k.RecoveryRequestsForDID(f.ctx, f.did)
		require.Empty(t, got, "every settled request must be gone from the index")
		return f.ctx.GasMeter().GasConsumed() - before
	}

	fewGas := measure(3)
	manyGas := measure(30)

	t.Logf("per-DID index scan gas: after 3 settle cycles = %d, after 30 = %d", fewGas, manyGas)
	require.Equal(t, fewGas, manyGas,
		"the per-DID recovery index scan must not grow with the number of settled requests")
}

// The lockout this prevents, stated as the property that matters: after a long griefing campaign the genuine owner can still open a recovery and carry it to execution.
func TestRecovery_OwnerCanStillRecoverAfterManySettledAttempts(t *testing.T) {
	f := setupRecovery(t)
	f.fundForRounds(t, 30)
	for i := 0; i < 30; i++ {
		f.initiateAndReject(t, i)
	}

	id := f.initiate(t, recoveryNonce)
	f.approveToThreshold(t, id)
	f.warpPastWindow()
	_, err := f.msg.ExecuteRecovery(f.ctx, &types.MsgExecuteRecovery{Creator: f.newCtrl, RecoveryId: id})
	require.NoError(t, err)

	doc, found := f.k.GetIdentity(f.ctx, f.did)
	require.True(t, found)
	require.Equal(t, f.newCtrl, doc.Controller, "the genuine owner recovered the identity")
	require.Equal(t, f.newKey, doc.PubKey)
	f.requireSettled(t, id)
}

// Pruning the record must NOT free the nonce.
func TestRecovery_PrunedRequestKeepsItsNonceBurned(t *testing.T) {
	f := setupRecovery(t)

	id := f.initiate(t, recoveryNonce)
	f.rejectToThreshold(t, id)
	f.requireSettled(t, id)

	_, err := f.msg.InitiateRecovery(f.ctx, f.initiateMsg(recoveryNonce, f.newKey))
	require.ErrorIs(t, err, types.ErrRecoveryNonceReused,
		"pruning a settled request must not un-burn its nonce")

	_, err = f.msg.InitiateRecovery(f.ctx, f.initiateMsg("a-fresh-nonce", f.newKey))
	require.NoError(t, err)
}

// The nonce markers are deliberately retained rather than pruned, so they must still be there — and still exported — after the requests that burned them are gone.
func TestRecovery_BurnedNoncesSurvivePruning(t *testing.T) {
	f := setupRecovery(t)
	for i := 0; i < 5; i++ {
		f.initiateAndReject(t, i)
	}

	require.Empty(t, f.k.RecoveryRequestsForDID(f.ctx, f.did))

	for i := 0; i < 5; i++ {
		_, err := f.msg.InitiateRecovery(f.ctx,
			f.initiateMsg(fmt.Sprintf("grief-nonce-%03d", i), pubFor(fmt.Sprintf("grief-key-%03d", i))))
		require.ErrorIs(t, err, types.ErrRecoveryNonceReused, "nonce %d must stay burned", i)
	}
}

// Settled requests are pruned and only PENDING ones were ever exported, so a live request still round-trips through genesis while the settled ones simply are not there to carry.
func TestRecovery_PruningLeavesLiveRequestsExportable(t *testing.T) {
	f := setupRecovery(t)
	for i := 0; i < 3; i++ {
		f.initiateAndReject(t, i)
	}
	live := f.initiate(t, recoveryNonce)

	gs := f.k.ExportGenesis(f.ctx)
	require.NoError(t, gs.Validate())
	require.Len(t, gs.RecoveryRequests, 1, "only the live request is exported")
	require.Equal(t, live, gs.RecoveryRequests[0].RecoveryId)
}
