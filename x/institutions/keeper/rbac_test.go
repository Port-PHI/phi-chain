// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

// mkAddr builds a 20-byte test address from a seed (padded/truncated).
func mkAddr(seed string) sdk.AccAddress {
	return sdk.AccAddress([]byte(fmt.Sprintf("%-20.20s", seed)))
}

// --- RBAC: role gate ---

func TestRBAC_OperatorServiceKeyCanMint(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 10000)
	svc := mkAddr("service_key")

	// Grant the operator role to the service key (single admin -> immediate execution).
	res, err := f.msg.GrantInstitutionRole(f.ctx, &types.MsgGrantInstitutionRole{
		Signer: f.oper.String(), Institution: "bank-a", Grantee: svc.String(), Role: types.INSTITUTION_ROLE_OPERATOR,
	})
	require.NoError(t, err)
	require.True(t, res.Executed)
	require.Equal(t, uint32(1), res.Approvals)
	require.Equal(t, uint32(1), res.Threshold)
	require.Equal(t, types.INSTITUTION_ROLE_OPERATOR, f.k.GetRole(f.ctx, "bank-a", svc))

	// The service key (operator) can mint automatically.
	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: svc.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "100", DepositRef: "dep-1",
	})
	require.NoError(t, err)
}

func TestRBAC_ViewerAndStrangerCannotMint(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 10000)
	viewer := mkAddr("viewer")
	stranger := mkAddr("stranger")

	_, err := f.msg.GrantInstitutionRole(f.ctx, &types.MsgGrantInstitutionRole{
		Signer: f.oper.String(), Institution: "bank-a", Grantee: viewer.String(), Role: types.INSTITUTION_ROLE_VIEWER,
	})
	require.NoError(t, err)

	// A viewer (read-only) cannot mint.
	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: viewer.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "100",
	})
	require.ErrorIs(t, err, types.ErrRoleNotAuthorized)

	// An address with no role cannot either.
	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: stranger.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "100",
	})
	require.ErrorIs(t, err, types.ErrRoleNotAuthorized)
}

func TestRBAC_RootAdminImplicit(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 10000)
	svc := mkAddr("service_key")

	// The registered admin, without an explicit grant, executes an ADMIN-only action (role grant)
	// -> meaning it has the implicit ADMIN role (RBAC root).
	res, err := f.msg.GrantInstitutionRole(f.ctx, &types.MsgGrantInstitutionRole{
		Signer: f.oper.String(), Institution: "bank-a", Grantee: svc.String(), Role: types.INSTITUTION_ROLE_OPERATOR,
	})
	require.NoError(t, err)
	require.True(t, res.Executed)

	// And a non-admin address cannot perform an ADMIN-only action.
	stranger := mkAddr("stranger")
	_, err = f.msg.GrantInstitutionRole(f.ctx, &types.MsgGrantInstitutionRole{
		Signer: stranger.String(), Institution: "bank-a", Grantee: svc.String(), Role: types.INSTITUTION_ROLE_VIEWER,
	})
	require.ErrorIs(t, err, types.ErrRoleNotAuthorized)
}

// --- Multisig (aggregated content-hash approval) ---

func TestMultisig_BootstrapSingleAdminExecutesImmediately(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 10000)
	svc := mkAddr("service_key")

	// Only one admin (root) exists -> effective threshold = 1 -> immediate execution (bootstrap anti-deadlock).
	res, err := f.msg.GrantInstitutionRole(f.ctx, &types.MsgGrantInstitutionRole{
		Signer: f.oper.String(), Institution: "bank-a", Grantee: svc.String(), Role: types.INSTITUTION_ROLE_OPERATOR,
	})
	require.NoError(t, err)
	require.True(t, res.Executed)
	require.Equal(t, uint32(1), res.Threshold)
}

func TestMultisig_TwoAdminsAggregateApprovals(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 10000)
	admin2 := mkAddr("second_admin")

	// Add a second admin (still single-admin -> immediate). From now on there are 2 admins.
	res, err := f.msg.GrantInstitutionRole(f.ctx, &types.MsgGrantInstitutionRole{
		Signer: f.oper.String(), Institution: "bank-a", Grantee: admin2.String(), Role: types.INSTITUTION_ROLE_ADMIN,
	})
	require.NoError(t, err)
	require.True(t, res.Executed)

	params := types.InstitutionParams{Caps: types.Caps{MintPerTx: "500"}}

	// First signature -> pending (1 of 2); not executed.
	r1, err := f.msg.UpdateInstitutionParams(f.ctx, &types.MsgUpdateInstitutionParams{
		Signer: f.oper.String(), Institution: "bank-a", Params: params,
	})
	require.NoError(t, err)
	require.False(t, r1.Executed)
	require.Equal(t, uint32(1), r1.Approvals)
	require.Equal(t, uint32(2), r1.Threshold)
	inst, _ := f.k.GetInstitution(f.ctx, "bank-a")
	require.Equal(t, "", inst.Params.Caps.MintPerTx, "must not apply before the threshold is reached")

	// Second signature over the same content -> execution.
	r2, err := f.msg.UpdateInstitutionParams(f.ctx, &types.MsgUpdateInstitutionParams{
		Signer: admin2.String(), Institution: "bank-a", Params: params,
	})
	require.NoError(t, err)
	require.True(t, r2.Executed)
	require.Equal(t, uint32(2), r2.Approvals)
	inst, _ = f.k.GetInstitution(f.ctx, "bank-a")
	require.Equal(t, "500", inst.Params.Caps.MintPerTx)
}

func TestMultisig_SameAdminTwiceDoesNotDoubleCount(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 10000)
	admin2 := mkAddr("second_admin")
	_, err := f.msg.GrantInstitutionRole(f.ctx, &types.MsgGrantInstitutionRole{
		Signer: f.oper.String(), Institution: "bank-a", Grantee: admin2.String(), Role: types.INSTITUTION_ROLE_ADMIN,
	})
	require.NoError(t, err)

	params := types.InstitutionParams{Caps: types.Caps{MintPerTx: "500"}}
	msg := &types.MsgUpdateInstitutionParams{Signer: f.oper.String(), Institution: "bank-a", Params: params}
	r1, err := f.msg.UpdateInstitutionParams(f.ctx, msg)
	require.NoError(t, err)
	require.False(t, r1.Executed)
	// The same admin again -> no distinct approval is added.
	r2, err := f.msg.UpdateInstitutionParams(f.ctx, msg)
	require.NoError(t, err)
	require.False(t, r2.Executed)
	require.Equal(t, uint32(1), r2.Approvals)
}

// An approval from an address that is no longer an ADMIN must not count, so shrinking the admin
// set cannot push a sub-threshold set of captured approvals over the now-lower threshold.
func TestMultisig_DeposedAdminApprovalDoesNotCount(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 10000)
	admin2 := mkAddr("admin2")
	admin3 := mkAddr("admin3")
	// Three admins (root + 2) with a 3-of-N sensitive threshold.
	f.k.SetRole(f.ctx, "bank-a", admin2, types.INSTITUTION_ROLE_ADMIN)
	f.k.SetRole(f.ctx, "bank-a", admin3, types.INSTITUTION_ROLE_ADMIN)
	inst, _ := f.k.GetInstitution(f.ctx, "bank-a")
	inst.Params.SensitiveThreshold = 3
	f.k.SetInstitution(f.ctx, inst)

	cfg := types.InstitutionAppConfig{DisplayNameFa: "X"}
	appcfg := func(signer sdk.AccAddress) *types.MsgUpdateInstitutionAppConfigResponse {
		r, err := f.msg.UpdateInstitutionAppConfig(f.ctx, &types.MsgUpdateInstitutionAppConfig{
			Signer: signer.String(), Institution: "bank-a", Config: cfg,
		})
		require.NoError(t, err)
		return r
	}

	// Capture two sub-threshold approvals (2 of 3).
	require.False(t, appcfg(admin2).Executed)
	r := appcfg(admin3)
	require.False(t, r.Executed)
	require.Equal(t, uint32(2), r.Approvals)
	require.Equal(t, uint32(3), r.Threshold)

	// Depose admin3; the admin set shrinks to 2 → the threshold drops to 2.
	f.k.DeleteRole(f.ctx, "bank-a", admin3)

	// admin2 re-touches the action: admin3's stale approval no longer counts, so it stays pending at
	// 1 of 2 (without the fix it would be 2 of 2 and execute against the lowered threshold).
	r = appcfg(admin2)
	require.False(t, r.Executed, "a deposed admin's approval must not push the action over the lowered threshold")
	require.Equal(t, uint32(1), r.Approvals)
	require.Equal(t, uint32(2), r.Threshold)
	inst, _ = f.k.GetInstitution(f.ctx, "bank-a")
	require.Empty(t, inst.AppConfig.DisplayNameFa, "the action must not have executed")

	// Two CURRENT admins can still execute the action legitimately.
	require.True(t, appcfg(f.oper).Executed)
}

// A successful admin-set change (here an ADMIN grant via the msg server) bumps the admin epoch,
// invalidating approvals captured under the previous epoch.
func TestMultisig_AdminSetChangeInvalidatesCapturedApprovals(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 10000)
	admin2 := mkAddr("admin2")
	admin3 := mkAddr("admin3")
	f.k.SetRole(f.ctx, "bank-a", admin2, types.INSTITUTION_ROLE_ADMIN) // root + admin2 → threshold 2

	cfg := types.InstitutionAppConfig{DisplayNameFa: "X"}
	// admin2 captures one approval for the app-config action (1 of 2).
	r, err := f.msg.UpdateInstitutionAppConfig(f.ctx, &types.MsgUpdateInstitutionAppConfig{
		Signer: admin2.String(), Institution: "bank-a", Config: cfg,
	})
	require.NoError(t, err)
	require.False(t, r.Executed)

	// A separate sensitive action — granting ADMIN to admin3 — reaches its threshold and changes the
	// admin set, bumping the epoch.
	_, err = f.msg.GrantInstitutionRole(f.ctx, &types.MsgGrantInstitutionRole{
		Signer: f.oper.String(), Institution: "bank-a", Grantee: admin3.String(), Role: types.INSTITUTION_ROLE_ADMIN,
	})
	require.NoError(t, err) // root: 1 of 2 (pending)
	gr, err := f.msg.GrantInstitutionRole(f.ctx, &types.MsgGrantInstitutionRole{
		Signer: admin2.String(), Institution: "bank-a", Grantee: admin3.String(), Role: types.INSTITUTION_ROLE_ADMIN,
	})
	require.NoError(t, err)
	require.True(t, gr.Executed) // admin2: 2 of 2 → admin3 added, epoch bumped

	// admin3 (a current admin) approves the app-config action: admin2's earlier approval is stale
	// (cast under the old epoch), so only admin3's fresh approval counts → still pending at 1 of 2.
	r, err = f.msg.UpdateInstitutionAppConfig(f.ctx, &types.MsgUpdateInstitutionAppConfig{
		Signer: admin3.String(), Institution: "bank-a", Config: cfg,
	})
	require.NoError(t, err)
	require.False(t, r.Executed, "approvals captured before the admin-set change must be invalidated (admin epoch advanced)")
	require.Equal(t, uint32(1), r.Approvals)
}

// --- AppConfig ---

func TestAppConfig_Update(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 10000)
	cfg := types.InstitutionAppConfig{
		Services:       types.AppServices{MintEnabled: true, RedeemEnabled: true},
		DisplayNameFa:  "Bank Mellat",
		LiveScanDomain: "scan.bankmellat.ir",
	}
	res, err := f.msg.UpdateInstitutionAppConfig(f.ctx, &types.MsgUpdateInstitutionAppConfig{
		Signer: f.oper.String(), Institution: "bank-a", Config: cfg,
	})
	require.NoError(t, err)
	require.True(t, res.Executed)

	inst, _ := f.k.GetInstitution(f.ctx, "bank-a")
	require.Equal(t, "Bank Mellat", inst.AppConfig.DisplayNameFa)
	require.True(t, inst.AppConfig.Services.MintEnabled)
	require.Equal(t, "scan.bankmellat.ir", inst.AppConfig.LiveScanDomain)
}

// --- Caps ---

func TestCaps_PerTxRejectsOversize(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 10000)
	_, err := f.msg.UpdateInstitutionParams(f.ctx, &types.MsgUpdateInstitutionParams{
		Signer: f.oper.String(), Institution: "bank-a", Params: types.InstitutionParams{Caps: types.Caps{MintPerTx: "500"}},
	})
	require.NoError(t, err)

	// Above the per-tx cap -> rejected.
	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.oper.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "600", DepositRef: "dep-1",
	})
	require.ErrorIs(t, err, types.ErrCapExceeded)

	// Below the cap -> allowed.
	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.oper.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "400", DepositRef: "dep-2",
	})
	require.NoError(t, err)
}

func TestCaps_DailyAccumulates(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 10000)
	_, err := f.msg.UpdateInstitutionParams(f.ctx, &types.MsgUpdateInstitutionParams{
		Signer: f.oper.String(), Institution: "bank-a", Params: types.InstitutionParams{Caps: types.Caps{MintDaily: "500"}},
	})
	require.NoError(t, err)

	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.oper.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "300", DepositRef: "dep-1",
	})
	require.NoError(t, err)
	// 300+300 = 600 > daily cap of 500 -> rejected.
	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.oper.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "300", DepositRef: "dep-2",
	})
	require.ErrorIs(t, err, types.ErrCapExceeded)
}

// TestCaps_RedeemDailyAccumulates guards the dead-counter bug: addRedeemCounters must run after a
// successful redeem so the daily redeem cap actually accumulates across transactions.
func TestCaps_RedeemDailyAccumulates(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 10000)
	// Mint 1000 toman to the holder so there is vault balance + uphi to redeem.
	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.oper.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "1000", DepositRef: "dep-1",
	})
	require.NoError(t, err)
	// Daily redeem cap = 500 toman.
	_, err = f.msg.UpdateInstitutionParams(f.ctx, &types.MsgUpdateInstitutionParams{
		Signer: f.oper.String(), Institution: "bank-a", Params: types.InstitutionParams{Caps: types.Caps{RedeemDaily: "500"}},
	})
	require.NoError(t, err)

	// First redeem of 300 toman -> ok.
	_, err = f.msg.InstitutionRedeem(f.ctx, &types.MsgInstitutionRedeem{
		Admin: f.holder.String(), Institution: "bank-a", Holder: f.holder.String(), AmountToman: "300", RedeemRef: "red-1",
	})
	require.NoError(t, err)
	// 300+300 = 600 > daily cap 500 -> rejected (counter now accumulates; previously dead).
	_, err = f.msg.InstitutionRedeem(f.ctx, &types.MsgInstitutionRedeem{
		Admin: f.holder.String(), Institution: "bank-a", Holder: f.holder.String(), AmountToman: "300", RedeemRef: "red-2",
	})
	require.ErrorIs(t, err, types.ErrCapExceeded)
}

// --- Tighten-only rule (redeem floor) ---

func TestParams_RedeemCapBelowFloorRejected(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 10000)
	// Protocol floor for the per-tx redeem cap = 100 toman.
	require.NoError(t, f.k.SetParams(f.ctx, types.Params{Operator: f.oper.String(), PhiToToman: 100_000, RedeemFloorPerTx: "100"}))

	// Redeem cap of 50 (< floor) -> rejected (user protection).
	_, err := f.msg.UpdateInstitutionParams(f.ctx, &types.MsgUpdateInstitutionParams{
		Signer: f.oper.String(), Institution: "bank-a", Params: types.InstitutionParams{Caps: types.Caps{RedeemPerTx: "50"}},
	})
	require.ErrorIs(t, err, types.ErrLooserThanFloor)

	// Cap of 150 (>= floor) -> allowed.
	res, err := f.msg.UpdateInstitutionParams(f.ctx, &types.MsgUpdateInstitutionParams{
		Signer: f.oper.String(), Institution: "bank-a", Params: types.InstitutionParams{Caps: types.Caps{RedeemPerTx: "150"}},
	})
	require.NoError(t, err)
	require.True(t, res.Executed)
}

// --- Idempotency ---

func TestIdempotency_DuplicateDepositRejected(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 10000)

	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.oper.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "100", DepositRef: "dep-1",
	})
	require.NoError(t, err)
	// Retrying the same deposit -> rejected (no double mint).
	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.oper.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "100", DepositRef: "dep-1",
	})
	require.ErrorIs(t, err, types.ErrDuplicateDeposit)
	// A different deposit -> allowed.
	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.oper.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "100", DepositRef: "dep-2",
	})
	require.NoError(t, err)
}

// --- Role grants genesis round-trip ---

func TestGenesis_RoleGrantsRoundTrip(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 10000)
	svc := mkAddr("service_key")
	_, err := f.msg.GrantInstitutionRole(f.ctx, &types.MsgGrantInstitutionRole{
		Signer: f.oper.String(), Institution: "bank-a", Grantee: svc.String(), Role: types.INSTITUTION_ROLE_OPERATOR,
	})
	require.NoError(t, err)

	gs := f.k.ExportGenesis(f.ctx)
	require.Len(t, gs.RoleGrants, 1)
	require.Equal(t, "bank-a", gs.RoleGrants[0].Institution)
	require.Equal(t, types.INSTITUTION_ROLE_OPERATOR, gs.RoleGrants[0].Role)
	require.NoError(t, gs.Validate())
}

// Institutions genesis Validate must reject duplicate role grants for the same
// (institution, address), so an imported genesis cannot carry conflicting grants for one sub-user.
func TestGenesisValidate_RejectsDuplicateRoleGrants(t *testing.T) {
	addr := mkAddr("dup_admin").String()
	gs := types.GenesisState{
		Params:       types.Params{PhiToToman: 100_000},
		Institutions: []types.Institution{{Id: "bank-a", InstitutionType: types.INSTITUTION_TYPE_FINANCIAL, VaultBalance: "0", AttestedReserve: "0"}},
		RoleGrants: []types.RoleGrant{
			{Institution: "bank-a", Address: addr, Role: types.INSTITUTION_ROLE_OPERATOR},
			{Institution: "bank-a", Address: addr, Role: types.INSTITUTION_ROLE_ADMIN},
		},
	}
	require.Error(t, gs.Validate(), "duplicate (institution, address) role grants must be rejected")

	gs.RoleGrants = gs.RoleGrants[:1]
	require.NoError(t, gs.Validate(), "a single grant per (institution, address) is valid")
}
