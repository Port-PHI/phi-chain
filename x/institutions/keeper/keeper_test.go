// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/phicrypto"
	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	"github.com/Port-PHI/phi-chain/x/institutions/keeper"
	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

type fakeBank struct {
	supply  map[string]math.Int
	bal     map[string]math.Int
	blocked map[string]bool
}

func newFakeBank() *fakeBank {
	return &fakeBank{supply: map[string]math.Int{}, bal: map[string]math.Int{}, blocked: map[string]bool{}}
}

// BlockedAddr reports whether an address is blocked from receiving funds.
func (b *fakeBank) BlockedAddr(addr sdk.AccAddress) bool { return b.blocked[addr.String()] }

func (b *fakeBank) get(m map[string]math.Int, k string) math.Int {
	if v, ok := m[k]; ok {
		return v
	}
	return math.ZeroInt()
}

func (b *fakeBank) MintCoins(_ context.Context, module string, amt sdk.Coins) error {
	a := amt.AmountOf(cointypes.Denom)
	b.supply[cointypes.Denom] = b.get(b.supply, cointypes.Denom).Add(a)
	b.bal[module] = b.get(b.bal, module).Add(a)
	return nil
}

func (b *fakeBank) BurnCoins(_ context.Context, module string, amt sdk.Coins) error {
	a := amt.AmountOf(cointypes.Denom)
	b.supply[cointypes.Denom] = b.get(b.supply, cointypes.Denom).Sub(a)
	b.bal[module] = b.get(b.bal, module).Sub(a)
	return nil
}

func (b *fakeBank) SendCoinsFromModuleToAccount(_ context.Context, module string, recip sdk.AccAddress, amt sdk.Coins) error {
	if b.blocked[recip.String()] {
		return fmt.Errorf("recipient %s is blocked", recip.String())
	}
	a := amt.AmountOf(cointypes.Denom)
	b.bal[module] = b.get(b.bal, module).Sub(a)
	b.bal[recip.String()] = b.get(b.bal, recip.String()).Add(a)
	return nil
}

func (b *fakeBank) SendCoinsFromAccountToModule(_ context.Context, sender sdk.AccAddress, module string, amt sdk.Coins) error {
	a := amt.AmountOf(cointypes.Denom)
	b.bal[sender.String()] = b.get(b.bal, sender.String()).Sub(a)
	b.bal[module] = b.get(b.bal, module).Add(a)
	return nil
}

// SendCoinsFromModuleToModule moves coin between module accounts.
func (b *fakeBank) SendCoinsFromModuleToModule(_ context.Context, from, to string, amt sdk.Coins) error {
	a := amt.AmountOf(cointypes.Denom)
	b.bal[from] = b.get(b.bal, from).Sub(a)
	b.bal[to] = b.get(b.bal, to).Add(a)
	return nil
}

func (b *fakeBank) GetSupply(_ context.Context, denom string) sdk.Coin {
	return sdk.NewCoin(denom, b.get(b.supply, denom))
}

func (b *fakeBank) GetBalance(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewCoin(denom, b.get(b.bal, addr.String()))
}

type fakeIdentity struct {
	dids          map[string]string
	nonActiveDIDs map[string]string
}

func (fakeIdentity) BootstrapPhase(sdk.Context) bool { return true }

func (f fakeIdentity) PrimaryDID(_ sdk.Context, controller string) (string, bool) {
	did, ok := f.dids[controller]
	return did, ok
}

func (f fakeIdentity) SubjectDID(_ sdk.Context, controller string) (string, bool) {
	if did, ok := f.dids[controller]; ok {
		return did, true
	}
	did, ok := f.nonActiveDIDs[controller]
	return did, ok
}

type fakeCoin struct{ penaltyBps int64 }

func (fakeCoin) AddCoins(sdk.Context, string, math.Int, int64) {}
func (c fakeCoin) EarlyRedeemPenalty(_ sdk.Context, _ string, redeemUphi math.Int) math.Int {
	if c.penaltyBps == 0 || !redeemUphi.IsPositive() {
		return math.ZeroInt()
	}
	return redeemUphi.MulRaw(c.penaltyBps).QuoRaw(int64(cointypes.BpsDenominator))
}

type fixture struct {
	ctx        sdk.Context
	k          keeper.Keeper
	msg        types.MsgServer
	bank       *fakeBank
	key        storetypes.StoreKey
	oper       sdk.AccAddress
	admin      sdk.AccAddress
	compliance sdk.AccAddress
	holder     sdk.AccAddress
	authority  string
}

func setup(t *testing.T) fixture {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_inst"))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())

	bank := newFakeBank()
	authority := sdk.AccAddress([]byte("gov_authority_______")).String()
	k := keeper.NewKeeper(cdc, key, authority, bank, fakeIdentity{}, fakeCoin{}, phicrypto.AcceptAll())

	oper := sdk.AccAddress([]byte("operator____________"))
	require.NoError(t, k.SetParams(testCtx.Ctx, types.Params{Operator: oper.String(), PhiToToman: 100_000, RedeemFloorPerTx: "100"}))

	return fixture{
		ctx:        testCtx.Ctx,
		k:          k,
		msg:        keeper.NewMsgServerImpl(k),
		bank:       bank,
		key:        key,
		oper:       oper,
		admin:      oper, // the operator is the institution admin
		compliance: sdk.AccAddress([]byte("compliance-officer__")),
		holder:     sdk.AccAddress([]byte("holder______________")),
		authority:  authority,
	}
}

func (f fixture) registerAndAttest(t *testing.T, id string, reserveToman int64) {
	t.Helper()
	_, err := f.msg.RegisterInstitution(f.ctx, &types.MsgRegisterInstitution{
		Operator: f.oper.String(), Id: id, License: "LIC-1", Admin: f.admin.String(),
		VaultAccount: "v", VaultApi: "x", Bond: "0",
		InstitutionType: types.INSTITUTION_TYPE_FINANCIAL,
	})
	require.NoError(t, err)
	f.k.SetRole(f.ctx, id, f.compliance, types.INSTITUTION_ROLE_COMPLIANCE)
	f.k.SetRole(f.ctx, id, secondAdmin(), types.INSTITUTION_ROLE_ADMIN)
	f.setSensitiveThreshold(t, id, 1)
	_, err = f.msg.PublishInstitutionAttestation(f.ctx, &types.MsgPublishInstitutionAttestation{
		Admin: f.compliance.String(), Institution: id, AttestedReserve: math.NewInt(reserveToman).String(),
	})
	require.NoError(t, err)
}

func (f fixture) setSensitiveThreshold(t *testing.T, id string, threshold uint32) {
	t.Helper()
	inst, found := f.k.GetInstitution(f.ctx, id)
	require.True(t, found)
	inst.Params.SensitiveThreshold = threshold
	f.k.SetInstitution(f.ctx, inst)
}

func (f fixture) registerAndAttestSoleAdmin(t *testing.T, id string, reserveToman int64) {
	t.Helper()
	_, err := f.msg.RegisterInstitution(f.ctx, &types.MsgRegisterInstitution{
		Operator: f.oper.String(), Id: id, License: "LIC-1", Admin: f.admin.String(),
		VaultAccount: "v", VaultApi: "x", Bond: "0",
		InstitutionType: types.INSTITUTION_TYPE_FINANCIAL,
	})
	require.NoError(t, err)
	f.k.SetRole(f.ctx, id, f.compliance, types.INSTITUTION_ROLE_COMPLIANCE)
	_, err = f.msg.PublishInstitutionAttestation(f.ctx, &types.MsgPublishInstitutionAttestation{
		Admin: f.compliance.String(), Institution: id, AttestedReserve: math.NewInt(reserveToman).String(),
	})
	require.NoError(t, err)
}

func secondAdmin() sdk.AccAddress { return sdk.AccAddress([]byte("second-admin-key____")) }

func (f fixture) registerAndAttestWithKycTiers(t *testing.T, id string, reserveToman int64, tiers []types.KycTierLimit) {
	t.Helper()
	f.registerAndAttest(t, id, reserveToman)
	inst, found := f.k.GetInstitution(f.ctx, id)
	require.True(t, found)
	inst.Params.KycTierLimits = tiers
	f.k.SetInstitution(f.ctx, inst)
}

func TestSolvencyInvariant_HoldsAfterMint(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 1000) // reserve 1000 toman

	res, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "1000", DepositRef: "dep-1",
	})
	require.NoError(t, err)
	require.Equal(t, "10000", res.MintedUphi)

	_, broken := keeper.SolvencyInvariant(f.k)(f.ctx)
	require.False(t, broken, "solvency invariant must hold with full backing")

	inst, _ := f.k.GetInstitution(f.ctx, "bank-a")
	require.Equal(t, "1000", inst.VaultBalance)
}

// A corrupt vault_balance must fail the solvency read CLOSED (panic), never silently read as 0: a silent 0 would understate the vault backing and could mask a shortfall.
func TestSumVaultBalance_FailsClosedOnCorruptVault(t *testing.T) {
	f := setup(t)
	f.k.SetInstitution(f.ctx, types.Institution{
		Id:              "bank-corrupt",
		VaultBalance:    "not-a-number",
		AttestedReserve: "0",
	})
	require.PanicsWithValue(t,
		`institution "bank-corrupt": unparseable vault_balance "not-a-number" (solvency read)`,
		func() { _ = f.k.SumVaultBalance(f.ctx) },
		"an unparseable vault_balance must panic on the solvency read, not read as 0")
}

func TestMint_RejectsExceedingBacking(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 500) // reserve only 500 toman

	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "600", DepositRef: "dep-1",
	})
	require.ErrorIs(t, err, types.ErrMintExceedsBacking)
}

func TestMint_RejectsWhenFrozen(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 1000)
	_, err := f.msg.FreezeInstitution(f.ctx, &types.MsgFreezeInstitution{Operator: f.oper.String(), Id: "bank-a", Frozen: true})
	require.NoError(t, err)

	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "100", DepositRef: "dep-1",
	})
	require.ErrorIs(t, err, types.ErrInstitutionFrozen)
}

func TestRedeem_KeepsSolvencyAndRejectsOverVault(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 1000)
	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "1000", DepositRef: "dep-1",
	})
	require.NoError(t, err)

	res, err := f.msg.InstitutionRedeem(f.ctx, &types.MsgInstitutionRedeem{
		Admin: f.holder.String(), Institution: "bank-a", Holder: f.holder.String(), AmountToman: "400", RedeemRef: "red-1",
	})
	require.NoError(t, err)
	require.Equal(t, "4000", res.BurnedUphi)

	_, broken := keeper.SolvencyInvariant(f.k)(f.ctx)
	require.False(t, broken, "redeem must preserve the solvency invariant")

	inst, _ := f.k.GetInstitution(f.ctx, "bank-a")
	require.Equal(t, "600", inst.VaultBalance)

	_, err = f.msg.InstitutionRedeem(f.ctx, &types.MsgInstitutionRedeem{
		Admin: f.holder.String(), Institution: "bank-a", Holder: f.holder.String(), AmountToman: "700", RedeemRef: "red-2",
	})
	require.ErrorIs(t, err, types.ErrInsufficientVault)
}

// TestRedeem_RejectsForceBurnByNonHolder covers the case where the bank keeper lets a module pull uphi from any account without that holder's signature, so without an explicit holder-consent guard an institution operator (the tx signer) could force-burn any holder's balance.
func TestRedeem_RejectsForceBurnByNonHolder(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 1000)
	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "1000", DepositRef: "dep-1",
	})
	require.NoError(t, err)

	_, err = f.msg.InstitutionRedeem(f.ctx, &types.MsgInstitutionRedeem{
		Admin: f.oper.String(), Institution: "bank-a", Holder: f.holder.String(), AmountToman: "400", RedeemRef: "red-1",
	})
	require.ErrorIs(t, err, types.ErrUnauthorized, "an operator must not force-burn a holder's balance")
}

// TestRedirectSlashedToPenalty_EscrowsOnBlockedDest covers the case where a blocked penalty destination must not halt the slashing BeginBlocker — the slashed delta is minted (supply restored) and held in the module account, with no error returned.
func TestRedirectSlashedToPenalty_EscrowsOnBlockedDest(t *testing.T) {
	f := setup(t)
	blocked := sdk.AccAddress([]byte("blocked_penalty_____"))
	f.bank.blocked[blocked.String()] = true
	require.NoError(t, f.k.SetParams(f.ctx, types.Params{Operator: f.oper.String(), PhiToToman: 100_000, RedeemFloorPerTx: "100", PenaltyDestination: blocked.String()}))

	before := f.bank.GetSupply(f.ctx, cointypes.Denom).Amount
	require.NoError(t, f.k.RedirectSlashedToPenalty(f.ctx, math.NewInt(5000)),
		"a blocked penalty destination must not halt the slash BeginBlocker")
	after := f.bank.GetSupply(f.ctx, cointypes.Denom).Amount
	require.Equal(t, before.Add(math.NewInt(5000)).String(), after.String(), "supply must be restored (solvency preserved)")
	require.Equal(t, "5000", f.bank.get(f.bank.bal, types.ModuleName).String(), "the compensation is escrowed in the module account")
}

// TestUpdateParams_RejectsBlockedPenaltyDestination covers the case where governance cannot set a blocked penalty destination (which would otherwise force the BeginBlock escrow path on every slash).
func TestUpdateParams_RejectsBlockedPenaltyDestination(t *testing.T) {
	f := setup(t)
	blocked := sdk.AccAddress([]byte("blocked_penalty_____"))
	f.bank.blocked[blocked.String()] = true
	_, err := f.msg.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority: f.authority,
		Params:    types.Params{Operator: f.oper.String(), PhiToToman: 100_000, RedeemFloorPerTx: "100", PenaltyDestination: blocked.String()},
	})
	require.ErrorIs(t, err, types.ErrInvalidParams, "a blocked penalty destination must be rejected at param-set")
}

// TestAttestation_RequiresComplianceNotOperator covers the separation-of-duties rule: ONLY the COMPLIANCE role may publish an attestation — not a pure OPERATOR, and DELIBERATELY not the ADMIN root (which is a minter).
func TestAttestation_RequiresComplianceNotOperator(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 1000) // a COMPLIANCE key attests (see the helper)
	opOnly := sdk.AccAddress([]byte("operator_only_______"))
	grant := func(role types.InstitutionRole) {
		_, err := f.msg.GrantInstitutionRole(f.ctx, &types.MsgGrantInstitutionRole{
			Institution: "bank-a", Signer: f.admin.String(), Grantee: opOnly.String(), Role: role,
		})
		require.NoError(t, err)
	}
	attest := func(signer string) error {
		_, err := f.msg.PublishInstitutionAttestation(f.ctx, &types.MsgPublishInstitutionAttestation{
			Admin: signer, Institution: "bank-a", AttestedReserve: "2000",
		})
		return err
	}
	require.ErrorIs(t, attest(f.admin.String()), types.ErrRoleNotAuthorized, "the admin (root) must not publish an attestation")
	grant(types.INSTITUTION_ROLE_OPERATOR)
	require.ErrorIs(t, attest(opOnly.String()), types.ErrRoleNotAuthorized, "an operator must not publish an attestation")
	grant(types.INSTITUTION_ROLE_COMPLIANCE) // overwrites the role grant
	require.NoError(t, attest(opOnly.String()), "a compliance officer may publish an attestation")
}

// TestLargeMint_RequiresMultisig covers the case where a mint at/above the governed threshold needs the institution's ADMIN multisig approvals; a smaller mint executes directly.
func TestLargeMint_RequiresMultisig(t *testing.T) {
	f := setup(t)
	require.NoError(t, f.k.SetParams(f.ctx, types.Params{Operator: f.oper.String(), PhiToToman: 100_000, RedeemFloorPerTx: "100", LargeMintThresholdToman: "500"}))
	f.registerAndAttest(t, "bank-a", 100000)

	admin2 := secondAdmin()
	f.setSensitiveThreshold(t, "bank-a", types.DefaultSensitiveThreshold)

	small, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "100", DepositRef: "small-1",
	})
	require.NoError(t, err)
	require.Equal(t, "1000", small.MintedUphi)

	large := &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "1000", DepositRef: "large-1",
	}
	res1, err := f.msg.InstitutionMint(f.ctx, large)
	require.NoError(t, err)
	require.Equal(t, "0", res1.MintedUphi, "large mint pends until the multisig threshold is reached")

	large2 := *large
	large2.Admin = admin2.String()
	res2, err := f.msg.InstitutionMint(f.ctx, &large2)
	require.NoError(t, err)
	require.Equal(t, "10000", res2.MintedUphi, "the second approval mints")
}

// TestMint_ProtocolCeiling covers the case where a mint above the protocol ceiling is rejected even when the institution sets no cap of its own.
func TestMint_ProtocolCeiling(t *testing.T) {
	f := setup(t)
	require.NoError(t, f.k.SetParams(f.ctx, types.Params{Operator: f.oper.String(), PhiToToman: 100_000, RedeemFloorPerTx: "100", MintCeilingPerTx: "500"}))
	f.registerAndAttest(t, "bank-a", 100000)
	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "600", DepositRef: "m1",
	})
	require.ErrorIs(t, err, types.ErrCapExceeded, "a mint above the protocol ceiling must be rejected")
	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "500", DepositRef: "m2",
	})
	require.NoError(t, err, "a mint at the ceiling is allowed")
}

func TestSolvencyInvariant_BreaksOnCorruption(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 1000)
	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "1000", DepositRef: "dep-1",
	})
	require.NoError(t, err)

	inst, _ := f.k.GetInstitution(f.ctx, "bank-a")
	inst.VaultBalance = "999"
	f.k.SetInstitution(f.ctx, inst)

	_, broken := keeper.SolvencyInvariant(f.k)(f.ctx)
	require.True(t, broken, "a vault shortfall must break the solvency invariant")
}

func TestNonNegativeVaultInvariant(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 1000)
	inst, _ := f.k.GetInstitution(f.ctx, "bank-a")
	inst.VaultBalance = "-5"
	f.k.SetInstitution(f.ctx, inst)

	_, broken := keeper.NonNegativeVaultInvariant(f.k)(f.ctx)
	require.True(t, broken, "a negative vault_balance must break the invariant")
}

// phi_to_toman must divide UphiPerPhi so the toman→uphi conversion is always integral; a non-divisor rate would make some mints non-integral and could halt the mint rail.
func TestParams_PhiToTomanMustDivideUphiPerPhi(t *testing.T) {
	for _, ok := range []uint64{1, 100_000, 200_000, 500_000, 1_000_000} {
		require.NoError(t, types.Params{PhiToToman: ok, RedeemFloorPerTx: "100"}.Validate(), "phi_to_toman=%d divides UphiPerPhi", ok)
	}
	for _, bad := range []uint64{7, 70_000, 99_999, 300_000} {
		require.Error(t, types.Params{PhiToToman: bad, RedeemFloorPerTx: "100"}.Validate(), "phi_to_toman=%d must be rejected (not a divisor)", bad)
	}
}

// deposit_ref/redeem_ref/fx_* are bounded in ValidateBasic so oversized attacker input cannot bloat the persistent KV keys/values they are written into.
func TestMsgValidateBasic_BoundsRefAndFxFieldLengths(t *testing.T) {
	addr := sdk.AccAddress([]byte("holder______________")).String()
	longRef := strings.Repeat("x", types.MaxRefLen+1)
	longFx := strings.Repeat("y", types.MaxFxFieldLen+1)

	mint := &types.MsgInstitutionMint{Admin: addr, Recipient: addr, Institution: "bank-a", AmountToman: "100", DepositRef: "ok"}
	require.NoError(t, mint.ValidateBasic())

	over := *mint
	over.DepositRef = longRef
	require.Error(t, over.ValidateBasic(), "oversized deposit_ref must be rejected")

	over = *mint
	over.FxTxRef = longFx
	require.Error(t, over.ValidateBasic(), "oversized fx_tx_ref must be rejected")

	redeem := &types.MsgInstitutionRedeem{Admin: addr, Holder: addr, Institution: "bank-a", AmountToman: "100", RedeemRef: longRef}
	require.Error(t, redeem.ValidateBasic(), "oversized redeem_ref must be rejected")
}

func TestMultiInstitutionSolvency(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 600)
	f.registerAndAttest(t, "bank-b", 400)
	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(), AmountToman: "600", DepositRef: "dep-1"})
	require.NoError(t, err)
	_, err = f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{Admin: f.admin.String(), Institution: "bank-b", Recipient: f.holder.String(), AmountToman: "400", DepositRef: "dep-1"})
	require.NoError(t, err)

	_, broken := keeper.SolvencyInvariant(f.k)(f.ctx)
	require.False(t, broken)
	require.Equal(t, math.NewInt(1000), f.k.SumVaultBalance(f.ctx))
}

func (f fixture) registerFxAndAttest(t *testing.T, id string, reserveToman int64) {
	t.Helper()
	_, err := f.msg.RegisterInstitution(f.ctx, &types.MsgRegisterInstitution{
		Operator: f.oper.String(), Id: id, License: "LIC-FX", Admin: f.admin.String(),
		VaultAccount: "v", VaultApi: "x", Bond: "0",
		InstitutionType: types.INSTITUTION_TYPE_FX,
	})
	require.NoError(t, err)
	f.k.SetRole(f.ctx, id, f.compliance, types.INSTITUTION_ROLE_COMPLIANCE)
	f.k.SetRole(f.ctx, id, secondAdmin(), types.INSTITUTION_ROLE_ADMIN)
	f.setSensitiveThreshold(t, id, 1)
	_, err = f.msg.PublishInstitutionAttestation(f.ctx, &types.MsgPublishInstitutionAttestation{
		Admin: f.compliance.String(), Institution: id, AttestedReserve: math.NewInt(reserveToman).String(),
	})
	require.NoError(t, err)
}

// An fx institution mints with Rial backing while recording fx provenance; the vault stays Rial and the global solvency invariant is unaffected by the provenance metadata.
func TestFxMint_RecordsProvenanceAndKeepsSolvency(t *testing.T) {
	f := setup(t)
	f.registerFxAndAttest(t, "exchange-1", 1000)

	inst, _ := f.k.GetInstitution(f.ctx, "exchange-1")
	require.Equal(t, types.INSTITUTION_TYPE_FX, inst.InstitutionType)

	res, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "exchange-1", Recipient: f.holder.String(),
		AmountToman: "1000", DepositRef: "dep-1", FxCurrency: "BTC", FxAmount: "3", FxTxRef: "0xabc",
	})
	require.NoError(t, err)
	require.Equal(t, "10000", res.MintedUphi)

	_, broken := keeper.SolvencyInvariant(f.k)(f.ctx)
	require.False(t, broken)
	inst, _ = f.k.GetInstitution(f.ctx, "exchange-1")
	require.Equal(t, "1000", inst.VaultBalance)
}

// An fx mint missing the provenance fields is rejected.
func TestFxMint_RequiresProvenanceMetadata(t *testing.T) {
	f := setup(t)
	f.registerFxAndAttest(t, "exchange-1", 1000)
	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "exchange-1", Recipient: f.holder.String(), AmountToman: "1000",
	})
	require.ErrorIs(t, err, types.ErrInvalidFxMetadata)
}

// A non-fx (financial/unspecified) institution must not carry fx provenance metadata.
func TestFinancialMint_RejectsFxMetadata(t *testing.T) {
	f := setup(t)
	f.registerAndAttest(t, "bank-a", 1000)
	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "bank-a", Recipient: f.holder.String(),
		AmountToman: "1000", FxCurrency: "BTC", FxAmount: "3", FxTxRef: "0xabc",
	})
	require.ErrorIs(t, err, types.ErrInvalidFxMetadata)
}

// An fx redeem records provenance and keeps the Rial vault/solvency consistent.
func TestFxRedeem_RecordsProvenanceAndKeepsSolvency(t *testing.T) {
	f := setup(t)
	f.registerFxAndAttest(t, "exchange-1", 1000)
	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: "exchange-1", Recipient: f.holder.String(),
		AmountToman: "1000", DepositRef: "dep-1", FxCurrency: "BTC", FxAmount: "3", FxTxRef: "0xdep",
	})
	require.NoError(t, err)

	res, err := f.msg.InstitutionRedeem(f.ctx, &types.MsgInstitutionRedeem{
		Admin: f.holder.String(), Institution: "exchange-1", Holder: f.holder.String(),
		AmountToman: "400", RedeemRef: "red-1", FxCurrency: "BTC", FxAmount: "1.2", FxTxRef: "0xred",
	})
	require.NoError(t, err)
	require.Equal(t, "4000", res.BurnedUphi)

	_, broken := keeper.SolvencyInvariant(f.k)(f.ctx)
	require.False(t, broken)
	inst, _ := f.k.GetInstitution(f.ctx, "exchange-1")
	require.Equal(t, "600", inst.VaultBalance)
}
