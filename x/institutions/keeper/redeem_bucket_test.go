// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"
	"time"

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

func setupBuckets(t *testing.T, capUphi string, dids, nonActive map[string]string) fixture {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_inst"))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())

	bank := newFakeBank()
	authority := sdk.AccAddress([]byte("gov_authority_______")).String()
	ident := fakeIdentity{dids: dids, nonActiveDIDs: nonActive}
	k := keeper.NewKeeper(cdc, key, authority, bank, ident, fakeCoin{}, phicrypto.AcceptAll())

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

func (f fixture) bucketTotals(t *testing.T, did string) (didBucket, sharedBucket string) {
	t.Helper()
	day := f.ctx.BlockTime().UTC().Unix() / 86_400
	read := func(kind byte, subject string) string {
		bz := f.ctx.KVStore(f.key).Get(types.RedeemSubjectCounterKey(day, kind, subject))
		if len(bz) == 0 {
			return "0"
		}
		return string(bz)
	}
	return read(types.RedeemSubjectDID, did), read(types.RedeemSubjectUnidentified, types.UnidentifiedRedeemSubject)
}

type bucketCase struct {
	name        string
	did         string // the DID this address holds, "" for none
	active      bool
	wantsShared bool
}

func bucketCases() []bucketCase {
	return []bucketCase{
		{name: "active DID", did: "did:phi:active-holder", active: true, wantsShared: false},
		{name: "existing but non-ACTIVE DID", did: "did:phi:frozen-holder", active: false, wantsShared: false},
		{name: "genuinely DID-less", did: "", wantsShared: true},
	}
}

// Each kind of redeeming address charges its own bucket and ONLY its own bucket.
func TestRedeemBucket_EveryAddressResolvesToExactlyOneBucket(t *testing.T) {
	for _, tc := range bucketCases() {
		t.Run(tc.name, func(t *testing.T) {
			holder := sdk.AccAddress([]byte("bucket-holder_______"))
			dids, nonActive := map[string]string{}, map[string]string{}
			switch {
			case tc.did == "":
			case tc.active:
				dids[holder.String()] = tc.did
			default:
				nonActive[holder.String()] = tc.did
			}

			f := setupBuckets(t, "200000000", dids, nonActive)
			f.registerAndAttest(t, "bank-a", 1_000_000)
			f.mintTo(t, "bank-a", holder, "10000", "dep-1")
			require.NoError(t, f.redeem("bank-a", holder, "5000", "red-1"))

			didBucket, shared := f.bucketTotals(t, tc.did)
			if tc.wantsShared {
				require.Equal(t, "0", didBucket, "a DID-less redemption must not write a DID bucket")
				require.NotEqual(t, "0", shared, "a DID-less redemption belongs in the shared bucket")
				return
			}
			require.NotEqual(t, "0", didBucket, "a DID holder must be charged its own bucket")
			require.Equal(t, "0", shared,
				"a DID holder must never touch the shared allowance reserved for DID-less holders")
		})
	}
}

// An exhausted DID bucket refuses the next redemption rather than spilling into the shared allowance.
func TestRedeemBucket_DIDHolderCannotDoubleDipTheSharedAllowance(t *testing.T) {
	holder := sdk.AccAddress([]byte("bucket-holder_______"))
	did := "did:phi:alice"
	f := setupBuckets(t, "50000", map[string]string{holder.String(): did}, nil)
	f.registerAndAttest(t, "bank-a", 1_000_000)
	f.mintTo(t, "bank-a", holder, "20000", "dep-1")

	require.NoError(t, f.redeem("bank-a", holder, "5000", "red-1"), "the first redemption fills the DID bucket")
	require.ErrorIs(t, f.redeem("bank-a", holder, "5000", "red-2"), types.ErrCapExceeded,
		"an exhausted DID bucket must refuse, never spill into the shared allowance")

	_, shared := f.bucketTotals(t, did)
	require.Equal(t, "0", shared, "the shared allowance must be untouched by a DID holder")
}

// A holder whose identity is frozen keeps the SAME bucket and cannot reach the shared allowance.
func TestRedeemBucket_SuspensionDoesNotMoveAHolderIntoTheSharedBucket(t *testing.T) {
	holder := sdk.AccAddress([]byte("bucket-holder_______"))
	did := "did:phi:alice"
	f := setupBuckets(t, "50000", map[string]string{holder.String(): did}, nil)
	f.registerAndAttest(t, "bank-a", 1_000_000)
	f.mintTo(t, "bank-a", holder, "20000", "dep-1")

	require.NoError(t, f.redeem("bank-a", holder, "3000", "red-1"))
	beforeDID, beforeShared := f.bucketTotals(t, did)
	require.Equal(t, "0", beforeShared)

	f = f.withIdentity(fakeIdentity{nonActiveDIDs: map[string]string{holder.String(): did}})

	require.NoError(t, f.redeem("bank-a", holder, "2000", "red-2"),
		"a frozen holder redeeming within their own remaining allowance is still within the cap")
	afterDID, afterShared := f.bucketTotals(t, did)
	require.NotEqual(t, beforeDID, afterDID, "the same DID bucket must have absorbed the second redemption")
	require.Equal(t, "0", afterShared, "suspension must not hand a holder the shared allowance")

	require.ErrorIs(t, f.redeem("bank-a", holder, "1000", "red-3"), types.ErrCapExceeded,
		"the pre- and post-suspension redemptions must accumulate in one bucket")
}

func (f fixture) withIdentity(ident fakeIdentity) fixture {
	return f.rebuild(ident, fakeCoin{})
}

func (f fixture) withPenalty(bps int64) fixture {
	return f.rebuild(fakeIdentity{}, fakeCoin{penaltyBps: bps})
}

func (f fixture) rebuild(ident fakeIdentity, coin fakeCoin) fixture {
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	k := keeper.NewKeeper(cdc, f.key, f.authority, f.bank, ident, coin, phicrypto.AcceptAll())
	f.k = k
	f.msg = keeper.NewMsgServerImpl(k)
	return f
}
