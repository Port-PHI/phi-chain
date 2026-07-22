// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"context"
	"errors"
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/phicrypto"
	"github.com/Port-PHI/phi-chain/x/institutions/keeper"
	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

type configurableIdentity struct{ bootstrap bool }

func (c configurableIdentity) BootstrapPhase(sdk.Context) bool { return c.bootstrap }

// No DID resolves here: the fx onboarding tests do not exercise the redeem cap.
func (configurableIdentity) PrimaryDID(sdk.Context, string) (string, bool) { return "", false }
func (configurableIdentity) SubjectDID(sdk.Context, string) (string, bool) { return "", false }

type fakeGov struct {
	proposals map[uint64]govv1.Proposal
}

func (g fakeGov) Proposal(_ context.Context, id uint64) (govv1.Proposal, error) {
	p, ok := g.proposals[id]
	if !ok {
		return govv1.Proposal{}, errors.New("proposal not found") // any error: fail-closed
	}
	return p, nil
}

func passedFxProposal(t *testing.T, id uint64, fxID string) govv1.Proposal {
	t.Helper()
	anyMsg, err := codectypes.NewAnyWithValue(&types.MsgFinalizeFxEntry{Operator: "gov", FxId: fxID})
	require.NoError(t, err)
	return govv1.Proposal{
		Id:       id,
		Status:   govv1.ProposalStatus_PROPOSAL_STATUS_PASSED,
		Messages: []*codectypes.Any{anyMsg},
	}
}

type fxFixture struct {
	ctx       sdk.Context
	k         keeper.Keeper
	msg       types.MsgServer
	oper      sdk.AccAddress
	applicant sdk.AccAddress
}

func fxSetup(t *testing.T, bootstrap bool, gov types.GovKeeper) fxFixture {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_fx"))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())

	bank := newFakeBank()
	authority := sdk.AccAddress([]byte("gov_authority_______")).String()
	k := keeper.NewKeeper(cdc, key, authority, bank, configurableIdentity{bootstrap: bootstrap}, fakeCoin{}, phicrypto.AcceptAll())
	if gov != nil {
		k.SetGovKeeper(gov)
	}

	oper := sdk.AccAddress([]byte("operator____________"))
	require.NoError(t, k.SetParams(testCtx.Ctx, types.Params{Operator: oper.String(), PhiToToman: 100_000, RedeemFloorPerTx: "100"}))

	return fxFixture{
		ctx:       testCtx.Ctx,
		k:         k,
		msg:       keeper.NewMsgServerImpl(k),
		oper:      oper,
		applicant: sdk.AccAddress([]byte("fx_applicant________")),
	}
}

func (f fxFixture) registerGuarantor(t *testing.T, id string) {
	t.Helper()
	f.k.SetInstitution(f.ctx, types.Institution{
		Id: id, License: "LIC-1", Admin: f.oper.String(),
		VaultAccount: "v", VaultApi: "x", Bond: "0",
		Status: types.INSTITUTION_STATUS_HEALTHY, VaultBalance: "0", AttestedReserve: "0",
		InstitutionType: types.INSTITUTION_TYPE_FINANCIAL,
	})
}

func (f fxFixture) request(id, guarantor string) error {
	_, err := f.msg.RequestFxEntry(f.ctx, &types.MsgRequestFxEntry{
		Applicant: f.applicant.String(), FxId: id, License: "EX-1", GuarantorId: guarantor,
	})
	return err
}

func (f fxFixture) guarantee(signer sdk.AccAddress, id string, approve bool) error {
	_, err := f.msg.GuaranteeFxEntry(f.ctx, &types.MsgGuaranteeFxEntry{
		GuarantorAdmin: signer.String(), FxId: id, Approve: approve,
	})
	return err
}

func (f fxFixture) finalize(id string, proposalID uint64) error {
	_, err := f.msg.FinalizeFxEntry(f.ctx, &types.MsgFinalizeFxEntry{
		Operator: f.oper.String(), FxId: id, ProposalId: proposalID,
	})
	return err
}

// Happy path during bootstrap: request → guarantee → finalize → fx institution exists.
func TestFxOnboarding_HappyPath_Bootstrap(t *testing.T) {
	f := fxSetup(t, true, nil)
	f.registerGuarantor(t, "bank-a")

	require.NoError(t, f.request("exchange-1", "bank-a"))
	req, ok := f.k.GetFxRequest(f.ctx, "exchange-1")
	require.True(t, ok)
	require.Equal(t, types.FxEntryStatus_FX_ENTRY_REQUESTED, req.Status)

	require.NoError(t, f.guarantee(f.oper, "exchange-1", true))
	req, _ = f.k.GetFxRequest(f.ctx, "exchange-1")
	require.Equal(t, types.FxEntryStatus_FX_ENTRY_GUARANTEED, req.Status)

	require.NoError(t, f.finalize("exchange-1", 0))

	inst, ok := f.k.GetInstitution(f.ctx, "exchange-1")
	require.True(t, ok)
	require.Equal(t, types.INSTITUTION_TYPE_FX, inst.InstitutionType)
	require.Equal(t, f.applicant.String(), inst.Admin)
	require.Equal(t, "0", inst.VaultBalance)
	require.False(t, f.k.HasFxRequest(f.ctx, "exchange-1"))
}

// A request must name a present, active financial guarantor; an exchange or a missing one is rejected.
func TestFxOnboarding_RequestRequiresActiveFinancialGuarantor(t *testing.T) {
	f := fxSetup(t, true, nil)

	require.ErrorIs(t, f.request("exchange-1", "ghost"), types.ErrGuarantorRequired)

	_, err := f.msg.RegisterInstitution(f.ctx, &types.MsgRegisterInstitution{
		Operator: f.oper.String(), Id: "exch", License: "L", Admin: f.oper.String(),
		VaultAccount: "v", VaultApi: "x", Bond: "0", InstitutionType: types.INSTITUTION_TYPE_FX,
	})
	require.NoError(t, err)
	require.ErrorIs(t, f.request("exchange-1", "exch"), types.ErrGuarantorRequired)

	f.registerGuarantor(t, "bank-a")
	_, err = f.msg.FreezeInstitution(f.ctx, &types.MsgFreezeInstitution{Operator: f.oper.String(), Id: "bank-a", Frozen: true})
	require.NoError(t, err)
	require.ErrorIs(t, f.request("exchange-1", "bank-a"), types.ErrGuarantorRequired)
}

// Only the guarantor institution's admin may sign the guarantee; decline clears the request.
func TestFxOnboarding_GuaranteeAuthorizationAndDecline(t *testing.T) {
	f := fxSetup(t, true, nil)
	f.registerGuarantor(t, "bank-a")
	require.NoError(t, f.request("exchange-1", "bank-a"))

	stranger := sdk.AccAddress([]byte("stranger____________"))
	require.ErrorIs(t, f.guarantee(stranger, "exchange-1", true), types.ErrRoleNotAuthorized)

	require.NoError(t, f.guarantee(f.oper, "exchange-1", false))
	require.False(t, f.k.HasFxRequest(f.ctx, "exchange-1"))
}

// Finalize requires a guaranteed request and rejects duplicates / premature finalize.
func TestFxOnboarding_FinalizeRequiresGuaranteed(t *testing.T) {
	f := fxSetup(t, true, nil)
	f.registerGuarantor(t, "bank-a")
	require.NoError(t, f.request("exchange-1", "bank-a"))

	require.ErrorIs(t, f.finalize("exchange-1", 0), types.ErrFxOnboarding)

	require.NoError(t, f.guarantee(f.oper, "exchange-1", true))
	require.NoError(t, f.finalize("exchange-1", 0))

	require.ErrorIs(t, f.request("exchange-1", "bank-a"), types.ErrInstitutionExists)
}

// Outside bootstrap, finalize requires a PASSED public proposal bound to this fx_id.
func TestFxOnboarding_NonBootstrapRequiresPassedProposal(t *testing.T) {
	gov := fakeGov{proposals: map[uint64]govv1.Proposal{
		7: passedFxProposal(t, 7, "exchange-1"),                           // passed + bound to this fx_id
		8: {Id: 8, Status: govv1.ProposalStatus_PROPOSAL_STATUS_REJECTED}, // not passed
		9: passedFxProposal(t, 9, "other-exchange"),                       // passed but binds a different fx_id
	}}
	f := fxSetup(t, false, gov)
	f.registerGuarantor(t, "bank-a")
	require.NoError(t, f.request("exchange-1", "bank-a"))
	require.NoError(t, f.guarantee(f.oper, "exchange-1", true))

	require.ErrorIs(t, f.finalize("exchange-1", 0), types.ErrFxOnboarding)
	require.ErrorIs(t, f.finalize("exchange-1", 99), types.ErrFxOnboarding)
	require.ErrorIs(t, f.finalize("exchange-1", 8), types.ErrFxOnboarding)
	require.ErrorIs(t, f.finalize("exchange-1", 9), types.ErrFxOnboarding)

	require.NoError(t, f.finalize("exchange-1", 7))
	inst, ok := f.k.GetInstitution(f.ctx, "exchange-1")
	require.True(t, ok)
	require.Equal(t, types.INSTITUTION_TYPE_FX, inst.InstitutionType)
}

// Outside bootstrap with no gov keeper wired, finalize fails closed.
func TestFxOnboarding_NonBootstrapFailsClosedWithoutGov(t *testing.T) {
	f := fxSetup(t, false, nil)
	f.registerGuarantor(t, "bank-a")
	require.NoError(t, f.request("exchange-1", "bank-a"))
	require.NoError(t, f.guarantee(f.oper, "exchange-1", true))
	require.ErrorIs(t, f.finalize("exchange-1", 7), types.ErrFxOnboarding)
}

// Genesis round-trips pending fx requests.
func TestFxOnboarding_GenesisRoundTrip(t *testing.T) {
	f := fxSetup(t, true, nil)
	f.registerGuarantor(t, "bank-a")
	require.NoError(t, f.request("exchange-1", "bank-a"))
	require.NoError(t, f.guarantee(f.oper, "exchange-1", true)) // GUARANTEED, still pending

	exported := f.k.ExportGenesis(f.ctx)
	require.NoError(t, exported.Validate())
	require.Len(t, exported.FxRequests, 1)
	require.Equal(t, "exchange-1", exported.FxRequests[0].FxId)
	require.Equal(t, types.FxEntryStatus_FX_ENTRY_GUARANTEED, exported.FxRequests[0].Status)

	f2 := fxSetup(t, true, nil)
	f2.k.InitGenesis(f2.ctx, *exported)
	req, ok := f2.k.GetFxRequest(f2.ctx, "exchange-1")
	require.True(t, ok)
	require.Equal(t, types.FxEntryStatus_FX_ENTRY_GUARANTEED, req.Status)
}
