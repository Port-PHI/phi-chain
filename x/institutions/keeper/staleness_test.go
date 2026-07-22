// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/phicrypto"
	"github.com/Port-PHI/phi-chain/x/institutions/keeper"
	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

const testStaleness = uint64(24 * 3600) // the default 24h floor

func setupStaleness(t *testing.T, floorSeconds uint64) fixture {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_inst"))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())

	bank := newFakeBank()
	authority := sdk.AccAddress([]byte("gov_authority_______")).String()
	k := keeper.NewKeeper(cdc, key, authority, bank, fakeIdentity{}, fakeCoin{}, phicrypto.AcceptAll())

	oper := sdk.AccAddress([]byte("operator____________"))
	ctx := testCtx.Ctx.WithBlockTime(time.Unix(1_700_000_000, 0).UTC())
	require.NoError(t, k.SetParams(ctx, types.Params{
		Operator: oper.String(), PhiToToman: 100_000, RedeemFloorPerTx: "100",
		MaxAttestationStalenessSeconds: floorSeconds,
	}))

	return fixture{
		ctx: ctx, k: k, msg: keeper.NewMsgServerImpl(k), bank: bank, key: key,
		oper: oper, admin: oper,
		compliance: sdk.AccAddress([]byte("compliance-officer__")),
		holder:     sdk.AccAddress([]byte("holder______________")),
		authority:  authority,
	}
}

func (f fixture) mintErr(inst, toman, ref string) error {
	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: inst, Recipient: f.holder.String(),
		AmountToman: toman, DepositRef: ref,
	})
	return err
}

func (f fixture) attest(t *testing.T, inst string, reserve int64) {
	t.Helper()
	f.k.SetRole(f.ctx, inst, f.compliance, types.INSTITUTION_ROLE_COMPLIANCE)
	_, err := f.msg.PublishInstitutionAttestation(f.ctx, &types.MsgPublishInstitutionAttestation{
		Admin: f.compliance.String(), Institution: inst, AttestedReserve: math.NewInt(reserve).String(),
	})
	require.NoError(t, err)
}

// THE CORE OF §4.6: a fresh attestation mints; one aged past the governed threshold does not; and a fresh attestation reopens minting all by itself — no MsgFreezeInstitution, no explicit unfreeze.
func TestStaleness_MintClosesWhenStaleAndSelfHealsOnAFreshAttestation(t *testing.T) {
	f := setupStaleness(t, testStaleness)
	f.registerAndAttest(t, "bank-a", 1_000_000)

	require.NoError(t, f.mintErr("bank-a", "1000", "dep-1"))

	f.ctx = f.ctx.WithBlockTime(f.ctx.BlockTime().Add(25 * time.Hour))
	err := f.mintErr("bank-a", "1000", "dep-2")
	require.ErrorIs(t, err, types.ErrAttestationStale,
		"minting must close once the reserve attestation is older than the governed threshold")

	inst, _ := f.k.GetInstitution(f.ctx, "bank-a")
	require.Equal(t, types.INSTITUTION_STATUS_HEALTHY, inst.Status,
		"the staleness gate must not flip Status — FROZEN belongs to the manual/divergence freezes")

	f.attest(t, "bank-a", 1_000_000)
	require.NoError(t, f.mintErr("bank-a", "1000", "dep-2"),
		"a fresh attestation reopens minting on its own")
}

// The boundary, through the real message path: exactly at the deadline mints; one second later does not.
func TestStaleness_BoundaryThroughTheMintPath(t *testing.T) {
	f := setupStaleness(t, testStaleness)
	f.registerAndAttest(t, "bank-a", 1_000_000)
	attestedAt := f.ctx.BlockTime()

	f.ctx = f.ctx.WithBlockTime(attestedAt.Add(24 * time.Hour))
	require.NoError(t, f.mintErr("bank-a", "1000", "dep-at-deadline"),
		"exactly at the deadline is still fresh")

	f.ctx = f.ctx.WithBlockTime(attestedAt.Add(24*time.Hour + time.Second))
	require.ErrorIs(t, f.mintErr("bank-a", "1000", "dep-past"), types.ErrAttestationStale,
		"one second past the deadline is stale")
}

// THE RED LINE (§4.9): redemption is NEVER gated by staleness.
func TestStaleness_RedeemStaysOpenWhenStale(t *testing.T) {
	f := setupStaleness(t, testStaleness)
	f.registerAndAttest(t, "bank-a", 1_000_000)

	require.NoError(t, f.mintErr("bank-a", "1000", "dep-1"))

	f.ctx = f.ctx.WithBlockTime(f.ctx.BlockTime().Add(30 * 24 * time.Hour))
	require.ErrorIs(t, f.mintErr("bank-a", "500", "dep-2"), types.ErrAttestationStale,
		"minting is closed")

	supplyBefore := f.bank.GetSupply(f.ctx, "uphi").Amount
	res, err := f.msg.InstitutionRedeem(f.ctx, &types.MsgInstitutionRedeem{
		Admin: f.holder.String(), Institution: "bank-a", Holder: f.holder.String(),
		AmountToman: "400", RedeemRef: "red-1",
	})
	require.NoError(t, err, "a stale attestation must NEVER block a user from getting their money back")
	require.Equal(t, "4000", res.BurnedUphi)
	require.Equal(t, supplyBefore.SubRaw(4_000).String(), f.bank.GetSupply(f.ctx, "uphi").Amount.String(),
		"the redeem accounting is untouched: supply falls by exactly the burned amount")

	_, broken := keeper.SolvencyInvariant(f.k)(f.ctx)
	require.False(t, broken, "the gate moves no value — solvency is unaffected")
}

// floor = 0 disables the gate: a months-old attestation still mints.
func TestStaleness_ZeroFloorDisablesTheGate(t *testing.T) {
	f := setupStaleness(t, 0)
	f.registerAndAttest(t, "bank-a", 1_000_000)

	f.ctx = f.ctx.WithBlockTime(f.ctx.BlockTime().Add(365 * 24 * time.Hour))
	require.NoError(t, f.mintErr("bank-a", "1000", "dep-1"),
		"max_attestation_staleness_seconds = 0 disables the floor entirely")

	_, broken := keeper.SolvencyInvariant(f.k)(f.ctx)
	require.False(t, broken)
}

// STRICTER-ONLY (§4.9), through the keeper: an institution may tighten its own latency below the protocol floor (and goes stale sooner); a latency looser than the floor is refused.
func TestStaleness_PerInstitutionLatencyIsStricterOnly(t *testing.T) {
	f := setupStaleness(t, testStaleness)
	f.registerAndAttest(t, "bank-a", 1_000_000)

	strict := types.InstitutionParams{}
	strict.AutoSuspendRules.MaxVaultAttestationLatencyS = 3600
	res, err := f.msg.UpdateInstitutionParams(f.ctx, &types.MsgUpdateInstitutionParams{
		Signer: f.admin.String(), Institution: "bank-a", Params: strict,
	})
	require.True(t, res.Executed, "the sensitive-action multisig executes with the lone implicit admin")
	require.NoError(t, err, "an institution may always make itself STRICTER")

	f.ctx = f.ctx.WithBlockTime(f.ctx.BlockTime().Add(2 * time.Hour))
	require.ErrorIs(t, f.mintErr("bank-a", "1000", "dep-1"), types.ErrAttestationStale,
		"the stricter institution limit binds: stale after 2h, long before the 24h floor")

	loose := types.InstitutionParams{}
	loose.AutoSuspendRules.MaxVaultAttestationLatencyS = testStaleness + 1
	_, err = f.msg.UpdateInstitutionParams(f.ctx, &types.MsgUpdateInstitutionParams{
		Signer: f.admin.String(), Institution: "bank-a", Params: loose,
	})
	require.ErrorIs(t, err, types.ErrLooserThanFloor,
		"an institution must never be able to loosen the protocol's staleness floor")
}

// INDEPENDENCE.
func TestStaleness_IsIndependentOfTheManualFreeze(t *testing.T) {
	f := setupStaleness(t, testStaleness)
	f.registerAndAttest(t, "bank-a", 1_000_000)
	inst, _ := f.k.GetInstitution(f.ctx, "bank-a")
	attestedAt := inst.LastAttestedAt
	require.Positive(t, attestedAt, "registration/attestation must stamp the clock, not leave it at the epoch")

	_, err := f.msg.FreezeInstitution(f.ctx, &types.MsgFreezeInstitution{
		Operator: f.oper.String(), Id: "bank-a", Frozen: true,
	})
	require.NoError(t, err)
	require.ErrorIs(t, f.mintErr("bank-a", "1000", "dep-1"), types.ErrInstitutionFrozen,
		"a manual freeze is still a manual freeze, reported as such")

	_, err = f.msg.FreezeInstitution(f.ctx, &types.MsgFreezeInstitution{
		Operator: f.oper.String(), Id: "bank-a", Frozen: false,
	})
	require.NoError(t, err)
	inst, _ = f.k.GetInstitution(f.ctx, "bank-a")
	require.Equal(t, attestedAt, inst.LastAttestedAt, "unfreezing must not re-stamp the attestation clock")
	require.NoError(t, f.mintErr("bank-a", "1000", "dep-1"), "unfrozen and still fresh -> mint is open")
}

// The clock is stamped at REGISTRATION, so a newly registered institution is not instantly stale.
func TestStaleness_RegistrationStampsTheClock(t *testing.T) {
	f := setupStaleness(t, testStaleness)
	_, err := f.msg.RegisterInstitution(f.ctx, &types.MsgRegisterInstitution{
		Operator: f.oper.String(), Id: "bank-new", License: "LIC-1", Admin: f.admin.String(),
		VaultAccount: "v", VaultApi: "x", Bond: "0",
		InstitutionType: types.INSTITUTION_TYPE_FINANCIAL,
	})
	require.NoError(t, err)

	inst, found := f.k.GetInstitution(f.ctx, "bank-new")
	require.True(t, found)
	require.Equal(t, f.ctx.BlockTime().Unix(), inst.LastAttestedAt,
		"registration starts the staleness window now, not at the epoch")
	require.False(t, inst.IsStaleAt(f.ctx.BlockTime().Unix(), testStaleness))
}

// Genesis round-trips last_attested_at, and stamps the block time when the genesis file left it unset — otherwise the bootstrap institution would be instantly stale and unable to mint at block 1.
func TestStaleness_GenesisRoundTrip(t *testing.T) {
	f := setupStaleness(t, testStaleness)
	const explicit = int64(1_699_000_000)

	gs := types.DefaultGenesis()
	gs.Params = types.Params{Operator: f.oper.String(), PhiToToman: 100_000, RedeemFloorPerTx: "100", MaxAttestationStalenessSeconds: testStaleness}
	gs.Institutions = []types.Institution{
		{
			Id: "seeded", License: "L", Admin: f.admin.String(), VaultAccount: "v", VaultApi: "x",
			Bond: "0", Status: types.INSTITUTION_STATUS_HEALTHY, VaultBalance: "0",
			AttestedReserve: "1000", InstitutionType: types.INSTITUTION_TYPE_FINANCIAL,
			LastAttestedAt: explicit,
		},
		{
			Id: "unstamped", License: "L", Admin: f.admin.String(), VaultAccount: "v", VaultApi: "x",
			Bond: "0", Status: types.INSTITUTION_STATUS_HEALTHY, VaultBalance: "0",
			AttestedReserve: "1000", InstitutionType: types.INSTITUTION_TYPE_FINANCIAL,
		},
	}
	require.NoError(t, gs.Validate())
	f.k.InitGenesis(f.ctx, *gs)

	seeded, _ := f.k.GetInstitution(f.ctx, "seeded")
	require.Equal(t, explicit, seeded.LastAttestedAt, "an explicit value round-trips verbatim")

	unstamped, _ := f.k.GetInstitution(f.ctx, "unstamped")
	require.Equal(t, f.ctx.BlockTime().Unix(), unstamped.LastAttestedAt,
		"an unset clock is stamped with the genesis block time, so the bootstrap institution can mint at block 1")
	require.False(t, unstamped.IsStaleAt(f.ctx.BlockTime().Unix(), testStaleness))

	out := f.k.ExportGenesis(f.ctx)
	byID := map[string]types.Institution{}
	for _, i := range out.Institutions {
		byID[i.Id] = i
	}
	require.Equal(t, explicit, byID["seeded"].LastAttestedAt)
	require.Equal(t, f.ctx.BlockTime().Unix(), byID["unstamped"].LastAttestedAt)
}

// Observability: the staleness is visible in the query WITHOUT being persisted, derived by the same code the mint gate uses.
func TestStaleness_SurfacedInTheQueryWithoutBeingPersisted(t *testing.T) {
	f := setupStaleness(t, testStaleness)
	f.registerAndAttest(t, "bank-a", 1_000_000)

	res, err := f.k.Institution(f.ctx, &types.QueryInstitutionRequest{Id: "bank-a"})
	require.NoError(t, err)
	require.False(t, res.AttestationStale)
	require.Equal(t, testStaleness, res.EffectiveStalenessSeconds)

	f.ctx = f.ctx.WithBlockTime(f.ctx.BlockTime().Add(25 * time.Hour))
	res, err = f.k.Institution(f.ctx, &types.QueryInstitutionRequest{Id: "bank-a"})
	require.NoError(t, err)
	require.True(t, res.AttestationStale, "the query reports staleness as the mint gate sees it")
	require.Equal(t, types.INSTITUTION_STATUS_HEALTHY, res.Institution.Status,
		"and it is DERIVED — the stored status is untouched")
}
