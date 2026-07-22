// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"fmt"
	"testing"

	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	"github.com/Port-PHI/phi-chain/x/institutions/keeper"
	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

func kycHolder(i int) sdk.AccAddress {
	return sdk.AccAddress([]byte(fmt.Sprintf("kyc-holder-%09d", i)))
}

func zeroVaultInst(id string) types.Institution {
	return types.Institution{
		Id: id, Admin: sdk.AccAddress([]byte("raw-root-admin______")).String(),
		InstitutionType: types.INSTITUTION_TYPE_FINANCIAL, VaultBalance: "0", AttestedReserve: "0",
	}
}

func countUnder(f fixture, prefix []byte) int {
	it := storetypes.KVStorePrefixIterator(f.ctx.KVStore(f.key), prefix)
	defer it.Close()
	n := 0
	for ; it.Valid(); it.Next() {
		n++
	}
	return n
}

func hasStoreEntry(entries []types.StoreEntry, key []byte) bool {
	for _, e := range entries {
		if string(e.Key) == string(key) {
			return true
		}
	}
	return false
}

func anyStoreEntryUnder(entries []types.StoreEntry, prefix []byte) bool {
	for _, e := range entries {
		if len(e.Key) >= len(prefix) && string(e.Key[:len(prefix)]) == string(prefix) {
			return true
		}
	}
	return false
}

func ceilDiv(a, b int) int { return (a + b - 1) / b }

// A removal with more ranged records than the budget drains over exactly ceil(n/budget) blocks and never deletes more than the budget in a single block.
func TestRemovalSweep_DrainsOverExactlyCeilBlocksNeverExceedingBudget(t *testing.T) {
	f := setup(t)
	const id = "drain-me"
	f.k.SetInstitution(f.ctx, zeroVaultInst(id))

	n := types.RemovalPruneBudget*2 + 7 // spans three budget-sized blocks: 512 + 512 + 7
	for i := 0; i < n; i++ {
		f.k.SetHolderKycTier(f.ctx, id, kycHolder(i), 1)
	}
	require.Equal(t, n, countUnder(f, types.HolderKycTierPrefixFor(id)), "precondition: n KYC records seeded")

	_, err := f.msg.RemoveInstitution(f.ctx, &types.MsgRemoveInstitution{Operator: f.oper.String(), Id: id})
	require.NoError(t, err)

	blocks := 0
	for f.k.HasPendingRemoval(f.ctx, id) {
		before := countUnder(f, types.HolderKycTierPrefixFor(id))
		f.k.SweepRemovals(f.ctx)
		deleted := before - countUnder(f, types.HolderKycTierPrefixFor(id))
		require.LessOrEqual(t, deleted, types.RemovalPruneBudget,
			"block %d deleted %d ranged records — the per-block budget is %d", blocks, deleted, types.RemovalPruneBudget)
		blocks++
		require.Less(t, blocks, n, "sweep is not making progress")
	}
	require.Equal(t, ceilDiv(n, types.RemovalPruneBudget), blocks,
		"a removal of %d records must drain in exactly ceil(%d/%d) blocks", n, n, types.RemovalPruneBudget)
	require.Zero(t, countUnder(f, types.HolderKycTierPrefixFor(id)), "every KYC record must be gone")
}

func TestRemovalSweep_InstitutionNonFunctionalImmediately(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 1000) // leaves role grants, so the drain has work and the id stays pending

	_, err := f.msg.RemoveInstitution(f.ctx, &types.MsgRemoveInstitution{Operator: f.oper.String(), Id: "bank-a"})
	require.NoError(t, err)
	require.True(t, f.k.HasPendingRemoval(f.ctx, "bank-a"), "the id is mid-drain (ranged records not yet swept)")
	require.False(t, f.k.HasInstitution(f.ctx, "bank-a"), "the registry record is already gone")

	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "100", DepositRef: "x"})
	require.ErrorIs(t, err, types.ErrInstitutionNotFound, "mint")

	_, err = f.msg.PublishInstitutionAttestation(f.ctx, &types.MsgPublishInstitutionAttestation{
		Admin: f.compliance.String(), Institution: "bank-a", AttestedReserve: "1"})
	require.ErrorIs(t, err, types.ErrInstitutionNotFound, "attest")

	_, err = f.msg.InstitutionRedeem(f.ctx, &types.MsgInstitutionRedeem{
		Admin: f.holder.String(), Institution: "bank-a", Holder: f.holder.String(), AmountToman: "1", RedeemRef: "x"})
	require.ErrorIs(t, err, types.ErrInstitutionNotFound, "redeem")

	_, err = f.msg.FreezeInstitution(f.ctx, &types.MsgFreezeInstitution{Operator: f.oper.String(), Id: "bank-a", Frozen: true})
	require.ErrorIs(t, err, types.ErrInstitutionNotFound, "freeze")

	_, err = f.msg.GrantInstitutionRole(f.ctx, &types.MsgGrantInstitutionRole{
		Institution: "bank-a", Signer: f.admin.String(), Grantee: f.compliance.String(), Role: types.INSTITUTION_ROLE_OPERATOR})
	require.ErrorIs(t, err, types.ErrInstitutionNotFound, "grant role")
}

func TestRemovalSweep_ReRegistrationBlockedWhileDraining(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "reuse-id", 1000)

	_, err := f.msg.RemoveInstitution(f.ctx, &types.MsgRemoveInstitution{Operator: f.oper.String(), Id: "reuse-id"})
	require.NoError(t, err)
	require.True(t, f.k.HasPendingRemoval(f.ctx, "reuse-id"))

	register := func() error {
		_, err := f.msg.RegisterInstitution(f.ctx, &types.MsgRegisterInstitution{
			Operator: f.oper.String(), Id: "reuse-id", License: "L", Admin: f.admin.String(),
			VaultAccount: "v", VaultApi: "x", InstitutionType: types.INSTITUTION_TYPE_FINANCIAL})
		return err
	}

	require.ErrorIs(t, register(), types.ErrRemovalInProgress, "RegisterInstitution")

	_, err = f.msg.RequestFxEntry(f.ctx, &types.MsgRequestFxEntry{
		FxId: "reuse-id", Applicant: f.admin.String(), License: "L", GuarantorId: "bank-g"})
	require.ErrorIs(t, err, types.ErrRemovalInProgress, "RequestFxEntry")

	f.k.SetFxRequest(f.ctx, types.FxEntryRequest{
		FxId: "reuse-id", Applicant: f.admin.String(), GuarantorId: "bank-g",
		Status: types.FxEntryStatus_FX_ENTRY_GUARANTEED})
	_, err = f.msg.FinalizeFxEntry(f.ctx, &types.MsgFinalizeFxEntry{Operator: f.oper.String(), FxId: "reuse-id"})
	require.ErrorIs(t, err, types.ErrRemovalInProgress, "FinalizeFxEntry")

	for f.k.HasPendingRemoval(f.ctx, "reuse-id") {
		f.k.SweepRemovals(f.ctx)
	}
	require.NoError(t, register(), "the id is registerable once the purge has fully drained")
}

func TestRemovalSweep_TwoNodeDeterminismWithConcurrentRemovals(t *testing.T) {
	seed := func(f fixture) {
		for _, s := range []struct {
			id string
			n  int
		}{{"inst-a", 600}, {"inst-b", 700}, {"inst-c", 50}} {
			f.k.SetInstitution(f.ctx, zeroVaultInst(s.id))
			for i := 0; i < s.n; i++ {
				f.k.SetHolderKycTier(f.ctx, s.id, kycHolder(i), 1)
			}
			_, err := f.msg.RemoveInstitution(f.ctx, &types.MsgRemoveInstitution{Operator: f.oper.String(), Id: s.id})
			require.NoError(t, err)
		}
	}

	f1, f2 := setup(t), setup(t)
	seed(f1)
	seed(f2)
	require.Equal(t, dumpModuleStore(f1.ctx, f1.key), dumpModuleStore(f2.ctx, f2.key), "initial states must match")

	pending := func(f fixture) bool {
		return f.k.HasPendingRemoval(f.ctx, "inst-a") ||
			f.k.HasPendingRemoval(f.ctx, "inst-b") || f.k.HasPendingRemoval(f.ctx, "inst-c")
	}
	for height := 0; pending(f1) || pending(f2); height++ {
		f1.k.SweepRemovals(f1.ctx)
		f2.k.SweepRemovals(f2.ctx)
		require.Equal(t, dumpModuleStore(f1.ctx, f1.key), dumpModuleStore(f2.ctx, f2.key),
			"stores diverged at height %d", height)
		require.Less(t, height, 10_000, "sweep did not converge")
	}
	require.False(t, pending(f1), "all removals drained")
}

func TestRemovalSweep_GenesisExportsMidDrainRemovalAsCompleted(t *testing.T) {
	f := setup(t)
	store := f.ctx.KVStore(f.key)
	contentHash := make([]byte, 32)
	sub := sdk.AccAddress([]byte("gone-subadmin_______"))
	holderG := sdk.AccAddress([]byte("gone-holder_________"))
	holderN := sdk.AccAddress([]byte("neighbor-holder_____"))

	f.k.SetInstitution(f.ctx, zeroVaultInst("gone"))
	f.k.SetRole(f.ctx, "gone", sub, types.INSTITUTION_ROLE_ADMIN)
	f.k.SetHolderKycTier(f.ctx, "gone", holderG, 3)
	store.Set(types.ApprovalKey("gone", contentHash, sub), make([]byte, 8))
	depositKey := types.DepositKey("gone", "mint", "R")
	counterKey := types.CounterTotalKey("gone", "md", 19_000)
	store.Set(depositKey, []byte{types.DepositMarkerByte})
	store.Set(counterKey, []byte("5000"))

	f.k.SetInstitution(f.ctx, zeroVaultInst("gone-neighbor"))
	f.k.SetRole(f.ctx, "gone-neighbor", sub, types.INSTITUTION_ROLE_ADMIN)
	f.k.SetHolderKycTier(f.ctx, "gone-neighbor", holderN, 2)

	_, err := f.msg.RemoveInstitution(f.ctx, &types.MsgRemoveInstitution{Operator: f.oper.String(), Id: "gone"})
	require.NoError(t, err)
	require.True(t, f.k.HasPendingRemoval(f.ctx, "gone"))
	require.NotEmpty(t, store.Get(types.HolderKycTierKey("gone", holderG)), "precondition: gone's KYC still present in live state")

	exported := f.k.ExportGenesis(f.ctx)
	require.NoError(t, exported.Validate())

	for _, rg := range exported.RoleGrants {
		require.NotEqual(t, "gone", rg.Institution, "gone's role grant must not be exported")
	}
	require.False(t, anyStoreEntryUnder(exported.Approvals, types.ApprovalInstitutionPrefixFor("gone")),
		"gone's approvals must not be exported")
	require.False(t, anyStoreEntryUnder(exported.StoreEntries, types.HolderKycTierPrefixFor("gone")),
		"gone's KYC tiers must not be exported")

	require.True(t, anyStoreEntryUnder(exported.StoreEntries, types.HolderKycTierPrefixFor("gone-neighbor")),
		"the neighbour's KYC must survive the filter")
	var neighborRole bool
	for _, rg := range exported.RoleGrants {
		if rg.Institution == "gone-neighbor" {
			neighborRole = true
		}
	}
	require.True(t, neighborRole, "the neighbour's role grant must survive the filter")

	require.True(t, hasStoreEntry(exported.DepositMarkers, depositKey), "the deposit marker must round-trip")
	require.True(t, hasStoreEntry(exported.CapCounters, counterKey), "the cap counter must round-trip")

	f2 := setup(t)
	f2.bank.supply[cointypes.Denom] = f.bank.GetSupply(f.ctx, cointypes.Denom).Amount
	require.NotPanics(t, func() { f2.k.InitGenesis(f2.ctx, *exported) })

	require.False(t, f2.k.HasInstitution(f2.ctx, "gone"))
	require.False(t, f2.k.HasPendingRemoval(f2.ctx, "gone"), "the queue is not carried, so import leaves it empty")
	_, tierGone := f2.k.HolderKycTier(f2.ctx, "gone", holderG)
	require.False(t, tierGone, "no orphan KYC tier for gone survives import")
	require.Equal(t, types.INSTITUTION_ROLE_UNSPECIFIED, f2.k.GetRole(f2.ctx, "gone", sub), "no orphan role for gone")

	tierN, okN := f2.k.HolderKycTier(f2.ctx, "gone-neighbor", holderN)
	require.True(t, okN, "the neighbour's KYC must survive import")
	require.Equal(t, uint32(2), tierN)

	require.NotEmpty(t, f2.ctx.KVStore(f2.key).Get(depositKey),
		"the anti-replay marker must survive, preserving replay protection across the genesis boundary")
	_, err = f2.msg.RegisterInstitution(f2.ctx, &types.MsgRegisterInstitution{
		Operator: f2.oper.String(), Id: "gone", License: "L", Admin: f2.admin.String(),
		VaultAccount: "v", VaultApi: "x", InstitutionType: types.INSTITUTION_TYPE_FINANCIAL})
	require.NoError(t, err, "the fully-removed id is registerable after import")
}

func TestRemovalSweep_SolvencyHoldsEveryBlock(t *testing.T) {
	f := setup(t)
	f.mintBacked(t, "live", 1000, "dep-1")
	f.k.SetInstitution(f.ctx, zeroVaultInst("dead"))
	for i := 0; i < types.RemovalPruneBudget+50; i++ {
		f.k.SetHolderKycTier(f.ctx, "dead", kycHolder(i), 1)
	}
	_, err := f.msg.RemoveInstitution(f.ctx, &types.MsgRemoveInstitution{Operator: f.oper.String(), Id: "dead"})
	require.NoError(t, err)

	for f.k.HasPendingRemoval(f.ctx, "dead") {
		_, broken := keeper.AllInvariants(f.k)(f.ctx)
		require.False(t, broken, "solvency must hold before a drain block")
		f.k.SweepRemovals(f.ctx)
		_, broken = keeper.AllInvariants(f.k)(f.ctx)
		require.False(t, broken, "solvency must hold after a drain block")
	}
}

func TestRemovalSweep_CapAndReplayStateSurviveRemoveAndReRegister(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "reuse", 1000)
	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "reuse", Recipient: f.holder.String(), AmountToman: "1000", DepositRef: "R"})
	require.NoError(t, err)
	_, err = f.msg.InstitutionRedeem(f.ctx, &types.MsgInstitutionRedeem{
		Admin: f.holder.String(), Institution: "reuse", Holder: f.holder.String(), AmountToman: "1000", RedeemRef: "RR"})
	require.NoError(t, err)

	day := f.ctx.BlockTime().Unix() / 86400
	require.NotEmpty(t, f.ctx.KVStore(f.key).Get(types.CounterTotalKey("reuse", "md", day)), "precondition: the mint counter exists")

	f.removeAndDrain(t, "reuse")

	_, err = f.msg.RegisterInstitution(f.ctx, &types.MsgRegisterInstitution{
		Operator: f.oper.String(), Id: "reuse", License: "L", Admin: f.admin.String(),
		VaultAccount: "v", VaultApi: "x", InstitutionType: types.INSTITUTION_TYPE_FINANCIAL})
	require.NoError(t, err)

	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "reuse", Recipient: f.holder.String(), AmountToman: "100", DepositRef: "R"})
	require.ErrorIs(t, err, types.ErrDuplicateDeposit, "removal must not un-burn the anti-replay marker")

	require.NotEmpty(t, f.ctx.KVStore(f.key).Get(types.CounterTotalKey("reuse", "md", day)),
		"the consumed daily cap counter must survive remove-and-re-register")
}

func TestRemovalSweep_DoubleRemoveFailsNotFound(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "once", 1000)

	_, err := f.msg.RemoveInstitution(f.ctx, &types.MsgRemoveInstitution{Operator: f.oper.String(), Id: "once"})
	require.NoError(t, err)
	require.True(t, f.k.HasPendingRemoval(f.ctx, "once"))

	_, err = f.msg.RemoveInstitution(f.ctx, &types.MsgRemoveInstitution{Operator: f.oper.String(), Id: "once"})
	require.ErrorIs(t, err, types.ErrInstitutionNotFound)
}
