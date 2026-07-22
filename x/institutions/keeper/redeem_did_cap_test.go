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

const capUphiForTest = "10000"

func setupDIDCap(t *testing.T, capUphi string, dids map[string]string) fixture {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_inst"))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())

	bank := newFakeBank()
	authority := sdk.AccAddress([]byte("gov_authority_______")).String()
	k := keeper.NewKeeper(cdc, key, authority, bank, fakeIdentity{dids: dids}, fakeCoin{}, phicrypto.AcceptAll())

	oper := sdk.AccAddress([]byte("operator____________"))
	ctx := testCtx.Ctx.WithBlockTime(time.Unix(1_700_000_000, 0).UTC())
	require.NoError(t, k.SetParams(ctx, types.Params{
		Operator: oper.String(), PhiToToman: 100_000, RedeemFloorPerTx: "100", RedeemDailyCapPerDidUphi: capUphi,
	}))

	return fixture{
		ctx: ctx, k: k, msg: keeper.NewMsgServerImpl(k), bank: bank, key: key,
		oper: oper, admin: oper,
		compliance: sdk.AccAddress([]byte("compliance-officer__")),
		holder:     sdk.AccAddress([]byte("holder______________")),
		authority:  authority,
	}
}

func (f fixture) mintTo(t *testing.T, inst string, holder sdk.AccAddress, toman, ref string) {
	t.Helper()
	_, err := f.msg.InstitutionMint(f.ctx, &types.MsgInstitutionMint{
		Admin: f.admin.String(), Institution: inst, Recipient: holder.String(),
		AmountToman: toman, KycTier: kycFundingTier, DepositRef: ref,
	})
	require.NoError(t, err)
}

func (f fixture) redeem(inst string, holder sdk.AccAddress, toman, ref string) error {
	_, err := f.msg.InstitutionRedeem(f.ctx, &types.MsgInstitutionRedeem{
		Admin: holder.String(), Institution: inst, Holder: holder.String(),
		AmountToman: toman, RedeemRef: ref,
	})
	return err
}

// A DID redeeming at or below the cap within a day is allowed.
func TestPerDIDRedeemCap_AtOrBelowTheCapIsAllowed(t *testing.T) {
	holder := sdk.AccAddress([]byte("holder______________"))
	f := setupDIDCap(t, capUphiForTest, map[string]string{holder.String(): "did:phi:alice"})
	f.registerAndAttest(t, "bank-a", 100_000)
	f.mintTo(t, "bank-a", holder, "5000", "dep-1") // 50,000 uphi

	require.NoError(t, f.redeem("bank-a", holder, "600", "red-1"))
	require.NoError(t, f.redeem("bank-a", holder, "400", "red-2"), "landing exactly ON the cap must be allowed")

	require.ErrorIs(t, f.redeem("bank-a", holder, "1", "red-3"), types.ErrCapExceeded)
}

// The per-DID cap aggregates across institutions, unlike the per-institution caps.
func TestPerDIDRedeemCap_AggregatesAcrossInstitutions(t *testing.T) {
	holder := sdk.AccAddress([]byte("holder______________"))
	f := setupDIDCap(t, capUphiForTest, map[string]string{holder.String(): "did:phi:alice"})
	f.registerAndAttest(t, "bank-a", 100_000)
	f.registerAndAttest(t, "bank-b", 100_000)
	f.mintTo(t, "bank-a", holder, "5000", "dep-a")
	f.mintTo(t, "bank-b", holder, "5000", "dep-b")

	supplyBefore := f.bank.GetSupply(f.ctx, "uphi").Amount

	require.NoError(t, f.redeem("bank-a", holder, "800", "red-a1"))

	err := f.redeem("bank-b", holder, "800", "red-b1")
	require.ErrorIs(t, err, types.ErrCapExceeded, "the second institution must not reset the human's daily cap")
	require.Contains(t, err.Error(), "across all institutions")

	require.NoError(t, f.redeem("bank-b", holder, "200", "red-b2"))

	require.Equal(t, supplyBefore.SubRaw(10_000), f.bank.GetSupply(f.ctx, "uphi").Amount,
		"only the two ALLOWED redemptions (8,000 + 2,000 uphi) burned; the rejected one moved nothing")
	_, broken := keeper.SolvencyInvariant(f.k)(f.ctx)
	require.False(t, broken, "the cap only gates - solvency is untouched")
}

// The cap is keyed by the HUMAN: two addresses under the same DID share one daily bucket.
func TestPerDIDRedeemCap_IsPerHumanNotPerAddress(t *testing.T) {
	first := sdk.AccAddress([]byte("alice-account-one___"))
	second := sdk.AccAddress([]byte("alice-account-two___"))
	f := setupDIDCap(t, capUphiForTest, map[string]string{
		first.String(): "did:phi:alice", second.String(): "did:phi:alice",
	})
	f.registerAndAttest(t, "bank-a", 100_000)
	f.mintTo(t, "bank-a", first, "5000", "dep-1")
	f.mintTo(t, "bank-a", second, "5000", "dep-2")

	require.NoError(t, f.redeem("bank-a", first, "900", "red-1")) // 9,000 uphi

	require.ErrorIs(t, f.redeem("bank-a", second, "900", "red-2"), types.ErrCapExceeded)
	require.NoError(t, f.redeem("bank-a", second, "100", "red-3"), "the remaining 1,000 uphi is spendable")
}

// FAIL CLOSED: a redeemer whose DID does not resolve is still capped, keyed on the address instead.
func TestPerDIDRedeemCap_FailsClosedToAddressKeyingWhenTheDIDIsUnknown(t *testing.T) {
	holder := sdk.AccAddress([]byte("holder______________"))
	f := setupDIDCap(t, capUphiForTest, nil) // no DID resolves for anyone
	f.registerAndAttest(t, "bank-a", 100_000)
	f.registerAndAttest(t, "bank-b", 100_000)
	f.mintTo(t, "bank-a", holder, "5000", "dep-a")
	f.mintTo(t, "bank-b", holder, "5000", "dep-b")

	require.NoError(t, f.redeem("bank-a", holder, "800", "red-a1"))
	require.ErrorIs(t, f.redeem("bank-b", holder, "800", "red-b1"), types.ErrCapExceeded,
		"an unresolved DID must NOT bypass the network-wide cap")
}

// The bucket is a block-time day index, so the cap resets deterministically in the next day.
func TestPerDIDRedeemCap_ResetsInTheNextDayBucket(t *testing.T) {
	holder := sdk.AccAddress([]byte("holder______________"))
	f := setupDIDCap(t, capUphiForTest, map[string]string{holder.String(): "did:phi:alice"})
	f.registerAndAttest(t, "bank-a", 100_000)
	f.mintTo(t, "bank-a", holder, "5000", "dep-1")

	require.NoError(t, f.redeem("bank-a", holder, "1000", "red-1")) // 10,000 uphi: the whole cap
	require.ErrorIs(t, f.redeem("bank-a", holder, "1", "red-2"), types.ErrCapExceeded)

	f.ctx = f.ctx.WithBlockTime(f.ctx.BlockTime().Add(24 * time.Hour))
	require.NoError(t, f.redeem("bank-a", holder, "1000", "red-3"), "the cap must reset in the next day bucket")
	require.ErrorIs(t, f.redeem("bank-a", holder, "1", "red-4"), types.ErrCapExceeded,
		"and the new day's allowance is itself capped")
}

// A cap of "0" (or unset) disables the network-wide check entirely.
func TestPerDIDRedeemCap_ZeroDisables(t *testing.T) {
	holder := sdk.AccAddress([]byte("holder______________"))
	f := setupDIDCap(t, "0", map[string]string{holder.String(): "did:phi:alice"})
	f.registerAndAttest(t, "bank-a", 100_000)
	f.mintTo(t, "bank-a", holder, "50000", "dep-1")

	require.NoError(t, f.redeem("bank-a", holder, "40000", "red-1"))
	_, broken := keeper.SolvencyInvariant(f.k)(f.ctx)
	require.False(t, broken)
}

// The default param is the settled 200 PHI, and it validates.
func TestPerDIDRedeemCap_DefaultIs200Phi(t *testing.T) {
	p := types.DefaultParams()
	require.NoError(t, p.Validate())
	require.Equal(t, "200000000", p.RedeemDailyCapPerDidUphi, "200 PHI = 200,000,000 uphi")
	require.Equal(t, math.NewInt(200_000_000), types.CapInt(p.RedeemDailyCapPerDidUphi))
}
