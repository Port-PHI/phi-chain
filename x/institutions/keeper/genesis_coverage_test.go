// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"encoding/binary"
	"testing"

	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/internal/storeprefix"
	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

type prefixCase struct {
	name   string
	prefix []byte
	exempt string
	seed   func(t *testing.T, f fixture)
}

func storePrefixCases() []prefixCase {
	contentHash := make([]byte, 32)
	epoch := func(n uint64) []byte {
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], n)
		return b[:]
	}
	return []prefixCase{
		{
			name: "params", prefix: types.ParamsKey,
			seed: func(t *testing.T, f fixture) { /* written by setup */ },
		},
		{
			name: "institutions", prefix: types.InstitutionPrefix,
			seed: func(t *testing.T, f fixture) { f.mintBacked(t, "bank-a", 1000, "dep-1") },
		},
		{
			name: "role_grants", prefix: types.RolePrefix,
			seed: func(t *testing.T, f fixture) {
				f.k.SetRole(f.ctx, "bank-a", f.compliance, types.INSTITUTION_ROLE_COMPLIANCE)
			},
		},
		{
			name: "approvals", prefix: types.ApprovalPrefix,
			seed: func(t *testing.T, f fixture) {
				f.ctx.KVStore(f.key).Set(types.ApprovalKey("bank-a", contentHash, f.oper), epoch(0))
			},
		},
		{
			name: "cap_counters", prefix: types.CounterPrefix,
			seed: func(t *testing.T, f fixture) { /* written by mintBacked */ },
		},
		{
			name: "deposit_markers", prefix: types.DepositPrefix,
			seed: func(t *testing.T, f fixture) { /* written by mintBacked */ },
		},
		{
			name: "fx_requests", prefix: types.FxRequestPrefix,
			seed: func(t *testing.T, f fixture) {
				f.k.SetFxRequest(f.ctx, types.FxEntryRequest{
					FxId:        "fx-pending",
					Applicant:   f.holder.String(),
					GuarantorId: "bank-a",
					Status:      types.FxEntryStatus_FX_ENTRY_REQUESTED,
				})
			},
		},
		{
			name: "admin_epoch", prefix: types.AdminEpochPrefix,
			seed: func(t *testing.T, f fixture) {
				f.ctx.KVStore(f.key).Set(types.AdminEpochKey("bank-a"), epoch(7))
			},
		},
		{
			name: "redeem_subject_counters", prefix: types.RedeemSubjectPrefix,
			seed: func(t *testing.T, f fixture) {
				f.ctx.KVStore(f.key).Set(
					types.RedeemSubjectCounterKey(19_000, types.RedeemSubjectDID, "did:phi:someone"),
					[]byte("4242"))
			},
		},
		{
			name: "holder_kyc_tiers", prefix: types.HolderKycTierPrefix,
			seed: func(t *testing.T, f fixture) { f.k.SetHolderKycTier(f.ctx, "bank-a", f.holder, 2) },
		},
		{
			name: "last_attestor", prefix: types.LastAttestorPrefix,
			seed: func(t *testing.T, f fixture) {
				f.ctx.KVStore(f.key).Set(types.LastAttestorKey("bank-a"), f.oper.Bytes())
			},
		},
		{
			name: "counter_prune_cursor", prefix: types.CounterPruneCursorPrefix,
			exempt: "a resumable ring cursor over the cap counters: losing it restarts the sweep from the " +
				"start of the keyspace and costs nothing but work. It carries no authority and gates no decision.",
			seed: func(t *testing.T, f fixture) {
				f.ctx.KVStore(f.key).Set(types.CounterPruneCursorKey(), []byte("cursor-position"))
			},
		},
		{
			name: "removal_queue", prefix: types.RemovalQueuePrefix,
			exempt: "a mid-drain removal is exported as already-completed (its ranged records are filtered " +
				"out and the queue is not carried), so the id comes back fully removed and re-registerable.",
			seed: func(t *testing.T, f fixture) {
				f.ctx.KVStore(f.key).Set(types.RemovalQueueKey("gone-inst"), []byte{types.RemovalQueueMarkerByte})
			},
		},
	}
}

// TestGenesis_RoundTripsEveryLiveStorePrefix seeds a record under EVERY prefix the keeper writes and asserts one export→import cycle reproduces the whole keyspace byte for byte, save the documented exemption.
func TestGenesis_RoundTripsEveryLiveStorePrefix(t *testing.T) {
	f := setup(t)
	cases := storePrefixCases()

	requireCasesCoverEveryDeclaredPrefix(t, cases)

	for _, tc := range cases {
		tc.seed(t, f)
	}

	before := dumpModuleStore(f.ctx, f.key)
	for _, tc := range cases {
		require.NotEmpty(t, keysUnder(before, tc.prefix), "prefix %s was not seeded", tc.name)
	}

	exported := f.k.ExportGenesis(f.ctx)
	require.NoError(t, exported.Validate())

	f2 := setup(t)
	f2.bank.supply[cointypes.Denom] = f.bank.GetSupply(f.ctx, cointypes.Denom).Amount
	require.NotPanics(t, func() { f2.k.InitGenesis(f2.ctx, *exported) })
	after := dumpModuleStore(f2.ctx, f2.key)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want, got := keysUnder(before, tc.prefix), keysUnder(after, tc.prefix)
			if tc.exempt != "" {
				require.Empty(t, got, "exempt prefix %s must not be imported: %s", tc.name, tc.exempt)
				return
			}
			require.Equal(t, want, got, "prefix %s did not survive export→import", tc.name)
		})
	}

	for _, tc := range cases {
		if tc.exempt != "" {
			for k := range keysUnder(before, tc.prefix) {
				delete(before, k)
			}
		}
	}
	require.Equal(t, before, after, "the module keyspace must round-trip in full")
}

// An approval retired when the admin-set epoch advanced must not revive across a genesis export→import.
func TestGenesis_RoundTripKeepsRevokedApprovalsRevoked(t *testing.T) {
	f := setup(t)
	f.mintBacked(t, "bank-a", 1000, "dep-1")

	contentHash := make([]byte, 32)
	store := f.ctx.KVStore(f.key)

	store.Set(types.ApprovalKey("bank-a", contentHash, f.oper), make([]byte, 8))
	var next [8]byte
	binary.BigEndian.PutUint64(next[:], 1)
	store.Set(types.AdminEpochKey("bank-a"), next[:])

	exported := f.k.ExportGenesis(f.ctx)
	require.NoError(t, exported.Validate())

	f2 := setup(t)
	f2.bank.supply[cointypes.Denom] = f.bank.GetSupply(f.ctx, cointypes.Denom).Amount
	f2.k.InitGenesis(f2.ctx, *exported)

	require.Equal(t, next[:], f2.ctx.KVStore(f2.key).Get(types.AdminEpochKey("bank-a")),
		"the admin-set epoch must survive genesis, or every retired approval revives")
	require.Equal(t, make([]byte, 8), f2.ctx.KVStore(f2.key).Get(types.ApprovalKey("bank-a", contentHash, f2.oper)),
		"the approval must be imported with the epoch it was actually cast under")
}

// A residual store entry may not name params, an institution, or any non-residual prefix.
func TestGenesis_RejectsAStoreEntryOutsideTheResidualKeyspace(t *testing.T) {
	for _, foreign := range [][]byte{
		types.ParamsKey, types.InstitutionPrefix, types.RolePrefix, types.ApprovalPrefix,
		types.CounterPrefix, types.DepositPrefix, types.FxRequestPrefix, types.CounterPruneCursorPrefix,
	} {
		f := setup(t)
		gs := types.DefaultGenesis()
		gs.Params = f.k.GetParams(f.ctx)
		gs.StoreEntries = []types.StoreEntry{{
			Key:   append(append([]byte(nil), foreign...), []byte("x")...),
			Value: []byte("forged"),
		}}
		require.Error(t, gs.Validate(), "prefix %X must be refused in store_entries", foreign)
		require.Panics(t, func() { f.k.InitGenesis(f.ctx, *gs) })
	}
}

func dumpModuleStore(ctx sdk.Context, key storetypes.StoreKey) map[string]string {
	out := map[string]string{}
	it := ctx.KVStore(key).Iterator(nil, nil)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		out[string(it.Key())] = string(it.Value())
	}
	return out
}

func keysUnder(dump map[string]string, prefix []byte) map[string]string {
	out := map[string]string{}
	for k, v := range dump {
		if len(k) >= len(prefix) && k[:len(prefix)] == string(prefix) {
			out[k] = v
		}
	}
	return out
}

func requireCasesCoverEveryDeclaredPrefix(t *testing.T, cases []prefixCase) {
	t.Helper()

	seeded := map[string]prefixCase{}
	for _, tc := range cases {
		seeded[string(tc.prefix)] = tc
	}

	for _, p := range types.AllStorePrefixes() {
		tc, ok := seeded[string(p.Bytes)]
		require.True(t, ok,
			"prefix %q is declared in AllStorePrefixes() but no case seeds it — an unseeded keyspace "+
				"round-trips vacuously", p.Name)

		dropped := p.Carry == storeprefix.CarryDropped
		require.Equal(t, dropped, tc.exempt != "",
			"prefix %q disagrees about whether genesis carries it: declaration says carried=%v", p.Name, !dropped)
		delete(seeded, string(p.Bytes))
	}

	for raw := range seeded {
		require.Fail(t, "a case seeds prefix %X, which AllStorePrefixes() does not declare", []byte(raw))
	}
}
