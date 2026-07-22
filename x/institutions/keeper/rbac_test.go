// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

func mkAddr(seed string) sdk.AccAddress {
	return sdk.AccAddress([]byte(fmt.Sprintf("%-20.20s", seed)))
}

func TestRBAC_OperatorServiceKeyCanMint(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 10000)
	svc := mkAddr("service_key")

	res, err := f.msg.GrantInstitutionRole(f.ctx, &types.MsgGrantInstitutionRole{
		Signer: f.oper.String(), Institution: "bank-a", Grantee: svc.String(), Role: types.INSTITUTION_ROLE_OPERATOR,
	})
	require.NoError(t, err)
	require.True(t, res.Executed)
	require.Equal(t, uint32(1), res.Approvals)
	require.Equal(t, uint32(1), res.Threshold)
	require.Equal(t, types.INSTITUTION_ROLE_OPERATOR, f.k.GetRole(f.ctx, "bank-a", svc))

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

	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: viewer.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "100",
	})
	require.ErrorIs(t, err, types.ErrRoleNotAuthorized)

	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: stranger.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "100",
	})
	require.ErrorIs(t, err, types.ErrRoleNotAuthorized)
}

func TestRBAC_RootAdminImplicit(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 10000)
	svc := mkAddr("service_key")

	res, err := f.msg.GrantInstitutionRole(f.ctx, &types.MsgGrantInstitutionRole{
		Signer: f.oper.String(), Institution: "bank-a", Grantee: svc.String(), Role: types.INSTITUTION_ROLE_OPERATOR,
	})
	require.NoError(t, err)
	require.True(t, res.Executed)

	stranger := mkAddr("stranger")
	_, err = f.msg.GrantInstitutionRole(f.ctx, &types.MsgGrantInstitutionRole{
		Signer: stranger.String(), Institution: "bank-a", Grantee: svc.String(), Role: types.INSTITUTION_ROLE_VIEWER,
	})
	require.ErrorIs(t, err, types.ErrRoleNotAuthorized)
}

func TestMultisig_BootstrapSingleAdminExecutesImmediately(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 10000)
	svc := mkAddr("service_key")

	res, err := f.msg.GrantInstitutionRole(f.ctx, &types.MsgGrantInstitutionRole{
		Signer: f.oper.String(), Institution: "bank-a", Grantee: svc.String(), Role: types.INSTITUTION_ROLE_OPERATOR,
	})
	require.NoError(t, err)
	require.True(t, res.Executed)
	require.Equal(t, uint32(1), res.Threshold)
}

func TestMultisig_TwoAdminsAggregateApprovals(t *testing.T) {
	f := setup(t)
	f.registerAndAttestSoleAdmin(t, "bank-a", 10000)
	admin2 := mkAddr("second_admin")

	res, err := f.msg.GrantInstitutionRole(f.ctx, &types.MsgGrantInstitutionRole{
		Signer: f.oper.String(), Institution: "bank-a", Grantee: admin2.String(), Role: types.INSTITUTION_ROLE_ADMIN,
	})
	require.NoError(t, err)
	require.True(t, res.Executed)

	params := types.InstitutionParams{Caps: types.Caps{MintPerTx: "500"}}

	r1, err := f.msg.UpdateInstitutionParams(f.ctx, &types.MsgUpdateInstitutionParams{
		Signer: f.oper.String(), Institution: "bank-a", Params: params,
	})
	require.NoError(t, err)
	require.False(t, r1.Executed)
	require.Equal(t, uint32(1), r1.Approvals)
	require.Equal(t, uint32(2), r1.Threshold)
	inst, _ := f.k.GetInstitution(f.ctx, "bank-a")
	require.Equal(t, "", inst.Params.Caps.MintPerTx, "must not apply before the threshold is reached")

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
	f.registerAndAttestSoleAdmin(t, "bank-a", 10000)
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
	r2, err := f.msg.UpdateInstitutionParams(f.ctx, msg)
	require.NoError(t, err)
	require.False(t, r2.Executed)
	require.Equal(t, uint32(1), r2.Approvals)
}

// An approval from an address that is no longer an ADMIN must not count.
func TestMultisig_DeposedAdminApprovalDoesNotCount(t *testing.T) {
	f := setup(t)
	f.registerAndAttestSoleAdmin(t, "bank-a", 10000)
	admin2 := mkAddr("admin2")
	admin3 := mkAddr("admin3")
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

	require.False(t, appcfg(admin2).Executed)
	r := appcfg(admin3)
	require.False(t, r.Executed)
	require.Equal(t, uint32(2), r.Approvals)
	require.Equal(t, uint32(3), r.Threshold)

	f.k.DeleteRole(f.ctx, "bank-a", admin3)

	r = appcfg(admin2)
	require.False(t, r.Executed, "a deposed admin's approval must not push the action over the lowered threshold")
	require.Equal(t, uint32(1), r.Approvals)
	require.Equal(t, uint32(2), r.Threshold)
	inst, _ = f.k.GetInstitution(f.ctx, "bank-a")
	require.Empty(t, inst.AppConfig.DisplayNameFa, "the action must not have executed")

	require.True(t, appcfg(f.oper).Executed)
}

// A successful admin-set change bumps the admin epoch, invalidating approvals from the previous epoch.
func TestMultisig_AdminSetChangeInvalidatesCapturedApprovals(t *testing.T) {
	f := setup(t)
	f.registerAndAttestSoleAdmin(t, "bank-a", 10000)
	admin2 := mkAddr("admin2")
	admin3 := mkAddr("admin3")
	f.k.SetRole(f.ctx, "bank-a", admin2, types.INSTITUTION_ROLE_ADMIN) // root + admin2 → threshold 2

	cfg := types.InstitutionAppConfig{DisplayNameFa: "X"}
	r, err := f.msg.UpdateInstitutionAppConfig(f.ctx, &types.MsgUpdateInstitutionAppConfig{
		Signer: admin2.String(), Institution: "bank-a", Config: cfg,
	})
	require.NoError(t, err)
	require.False(t, r.Executed)

	_, err = f.msg.GrantInstitutionRole(f.ctx, &types.MsgGrantInstitutionRole{
		Signer: f.oper.String(), Institution: "bank-a", Grantee: admin3.String(), Role: types.INSTITUTION_ROLE_ADMIN,
	})
	require.NoError(t, err) // root: 1 of 2 (pending)
	gr, err := f.msg.GrantInstitutionRole(f.ctx, &types.MsgGrantInstitutionRole{
		Signer: admin2.String(), Institution: "bank-a", Grantee: admin3.String(), Role: types.INSTITUTION_ROLE_ADMIN,
	})
	require.NoError(t, err)
	require.True(t, gr.Executed) // admin2: 2 of 2 → admin3 added, epoch bumped

	r, err = f.msg.UpdateInstitutionAppConfig(f.ctx, &types.MsgUpdateInstitutionAppConfig{
		Signer: admin3.String(), Institution: "bank-a", Config: cfg,
	})
	require.NoError(t, err)
	require.False(t, r.Executed, "approvals captured before the admin-set change must be invalidated (admin epoch advanced)")
	require.Equal(t, uint32(1), r.Approvals)
}

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

func TestCaps_PerTxRejectsOversize(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 10000)
	_, err := f.msg.UpdateInstitutionParams(f.ctx, &types.MsgUpdateInstitutionParams{
		Signer: f.oper.String(), Institution: "bank-a", Params: types.InstitutionParams{Caps: types.Caps{MintPerTx: "500"}},
	})
	require.NoError(t, err)

	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.oper.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "600", DepositRef: "dep-1",
	})
	require.ErrorIs(t, err, types.ErrCapExceeded)

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
	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.oper.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "300", DepositRef: "dep-2",
	})
	require.ErrorIs(t, err, types.ErrCapExceeded)
}

// The daily redeem cap must accumulate across transactions (guards a dead-counter bug).
func TestCaps_RedeemDailyAccumulates(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 10000)
	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.oper.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "1000", DepositRef: "dep-1",
	})
	require.NoError(t, err)
	_, err = f.msg.UpdateInstitutionParams(f.ctx, &types.MsgUpdateInstitutionParams{
		Signer: f.oper.String(), Institution: "bank-a", Params: types.InstitutionParams{Caps: types.Caps{RedeemDaily: "500"}},
	})
	require.NoError(t, err)

	_, err = f.msg.InstitutionRedeem(f.ctx, &types.MsgInstitutionRedeem{
		Admin: f.holder.String(), Institution: "bank-a", Holder: f.holder.String(), AmountToman: "300", RedeemRef: "red-1",
	})
	require.NoError(t, err)
	_, err = f.msg.InstitutionRedeem(f.ctx, &types.MsgInstitutionRedeem{
		Admin: f.holder.String(), Institution: "bank-a", Holder: f.holder.String(), AmountToman: "300", RedeemRef: "red-2",
	})
	require.ErrorIs(t, err, types.ErrCapExceeded)
}

func TestParams_RedeemCapBelowFloorRejected(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 10000)
	require.NoError(t, f.k.SetParams(f.ctx, types.Params{Operator: f.oper.String(), PhiToToman: 100_000, RedeemFloorPerTx: "100"}))

	_, err := f.msg.UpdateInstitutionParams(f.ctx, &types.MsgUpdateInstitutionParams{
		Signer: f.oper.String(), Institution: "bank-a", Params: types.InstitutionParams{Caps: types.Caps{RedeemPerTx: "50"}},
	})
	require.ErrorIs(t, err, types.ErrLooserThanFloor)

	res, err := f.msg.UpdateInstitutionParams(f.ctx, &types.MsgUpdateInstitutionParams{
		Signer: f.oper.String(), Institution: "bank-a", Params: types.InstitutionParams{Caps: types.Caps{RedeemPerTx: "150"}},
	})
	require.NoError(t, err)
	require.True(t, res.Executed)
}

func TestIdempotency_DuplicateDepositRejected(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 10000)

	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.oper.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "100", DepositRef: "dep-1",
	})
	require.NoError(t, err)
	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.oper.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "100", DepositRef: "dep-1",
	})
	require.ErrorIs(t, err, types.ErrDuplicateDeposit)
	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.oper.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "100", DepositRef: "dep-2",
	})
	require.NoError(t, err)
}

func TestGenesis_RoleGrantsRoundTrip(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 10000)
	svc := mkAddr("service_key")
	_, err := f.msg.GrantInstitutionRole(f.ctx, &types.MsgGrantInstitutionRole{
		Signer: f.oper.String(), Institution: "bank-a", Grantee: svc.String(), Role: types.INSTITUTION_ROLE_OPERATOR,
	})
	require.NoError(t, err)

	gs := f.k.ExportGenesis(f.ctx)
	roles := map[string]types.InstitutionRole{}
	for _, rg := range gs.RoleGrants {
		require.Equal(t, "bank-a", rg.Institution)
		roles[rg.Address] = rg.Role
	}
	require.Len(t, gs.RoleGrants, 3)
	require.Equal(t, types.INSTITUTION_ROLE_COMPLIANCE, roles[f.compliance.String()], "the COMPLIANCE attester grant must round-trip")
	require.Equal(t, types.INSTITUTION_ROLE_OPERATOR, roles[svc.String()], "the OPERATOR service-key grant must round-trip")
	require.NoError(t, gs.Validate())
}

// Genesis Validate must reject duplicate (institution, address) role grants.
func TestGenesisValidate_RejectsDuplicateRoleGrants(t *testing.T) {
	addr := mkAddr("dup_admin").String()
	gs := types.GenesisState{
		Params: types.Params{PhiToToman: 100_000, RedeemFloorPerTx: "100"},
		Institutions: []types.Institution{{
			Id: "bank-a", Admin: someInstAddr("rbac-root-admin_____"),
			InstitutionType: types.INSTITUTION_TYPE_FINANCIAL, VaultBalance: "0", AttestedReserve: "0",
		}},
		RoleGrants: []types.RoleGrant{
			{Institution: "bank-a", Address: addr, Role: types.INSTITUTION_ROLE_OPERATOR},
			{Institution: "bank-a", Address: addr, Role: types.INSTITUTION_ROLE_ADMIN},
		},
	}
	require.Error(t, gs.Validate(), "duplicate (institution, address) role grants must be rejected")

	gs.RoleGrants = gs.RoleGrants[:1]
	require.NoError(t, gs.Validate(), "a single grant per (institution, address) is valid")
}

func someInstAddr(label string) string {
	b := make([]byte, 20)
	copy(b, label)
	return sdk.AccAddress(b).String()
}
