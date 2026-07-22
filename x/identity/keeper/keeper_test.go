// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"bytes"
	"context"
	"crypto/elliptic"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/phicrypto"
	"github.com/Port-PHI/phi-chain/x/identity/keeper"
	"github.com/Port-PHI/phi-chain/x/identity/types"
)

const testIssuerDID = "did:phi:issuer"

const testChainID = "phi-testnet-1"

type fakeBank struct {
	balances map[string]math.Int
}

func newFakeBank() *fakeBank { return &fakeBank{balances: map[string]math.Int{}} }

func accKey(a sdk.AccAddress) string { return "acc:" + a.String() }
func modKey(m string) string         { return "mod:" + m }

func (b *fakeBank) get(k string) math.Int {
	if v, ok := b.balances[k]; ok {
		return v
	}
	return math.ZeroInt()
}

// Fund credits an account.
func (b *fakeBank) Fund(a sdk.AccAddress, amt math.Int) {
	b.balances[accKey(a)] = b.get(accKey(a)).Add(amt)
}

func (b *fakeBank) BalanceOf(a sdk.AccAddress) math.Int { return b.get(accKey(a)) }
func (b *fakeBank) ModuleBalance(m string) math.Int     { return b.get(modKey(m)) }

// Total is the ledger-wide sum.
func (b *fakeBank) Total() math.Int {
	sum := math.ZeroInt()
	for _, v := range b.balances {
		sum = sum.Add(v)
	}
	return sum
}

func (b *fakeBank) move(from, to string, amt sdk.Coins) error {
	v := amt.AmountOf(cointypes.Denom)
	if b.get(from).LT(v) {
		return fmt.Errorf("insufficient funds: %s has %s, needs %s", from, b.get(from), v)
	}
	b.balances[from] = b.get(from).Sub(v)
	b.balances[to] = b.get(to).Add(v)
	return nil
}

func (b *fakeBank) SendCoinsFromAccountToModule(_ context.Context, sender sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	return b.move(accKey(sender), modKey(recipientModule), amt)
}

func (b *fakeBank) SendCoinsFromModuleToAccount(_ context.Context, senderModule string, recipient sdk.AccAddress, amt sdk.Coins) error {
	return b.move(modKey(senderModule), accKey(recipient), amt)
}

func (b *fakeBank) SendCoinsFromModuleToModule(_ context.Context, senderModule, recipientModule string, amt sdk.Coins) error {
	return b.move(modKey(senderModule), modKey(recipientModule), amt)
}

func setupIdentityFull(t *testing.T, v phicrypto.Verifier) (sdk.Context, keeper.Keeper, types.MsgServer, *fakeBank) {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_id"))
	ctx := testCtx.Ctx.WithChainID(testChainID)
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	authority := sdk.AccAddress([]byte("gov_authority_______")).String()
	bank := newFakeBank()
	k := keeper.NewKeeper(cdc, key, authority, v, bank)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))
	k.SetTrustedIssuer(ctx, types.TrustedIssuer{Did: testIssuerDID, PubKey: []byte("issuer-pk"), Active: true})
	return ctx, k, keeper.NewMsgServerImpl(k), bank
}

func setupIdentityV(t *testing.T, v phicrypto.Verifier) (sdk.Context, keeper.Keeper, types.MsgServer) {
	ctx, k, msg, _ := setupIdentityFull(t, v)
	return ctx, k, msg
}

func setupIdentity(t *testing.T) (sdk.Context, keeper.Keeper, types.MsgServer) {
	return setupIdentityV(t, phicrypto.AcceptAll())
}

func pubFor(label string) []byte {
	scalar := sha256.Sum256([]byte("phi-test-p256-" + label))
	x, y := elliptic.P256().ScalarBaseMult(scalar[:])
	return elliptic.Marshal(elliptic.P256(), x, y)
}
func didFor(label string) string {
	did, err := types.DeriveDIDFromP256(pubFor(label))
	if err != nil {
		panic(err)
	}
	return did
}

func reg(creator, label string, uniq []byte) *types.MsgRegisterIdentity {
	return &types.MsgRegisterIdentity{
		Creator: creator, Did: didFor(label), PubKey: pubFor(label), UniquenessHash: uniq,
		IssuerDid: testIssuerDID, IssuerSig: []byte("isig"),
		Nonce: []byte("nonce-" + label), PopSig: []byte("pop-" + label),
	}
}

func TestRegisterIdentity_Uniqueness(t *testing.T) {
	ctx, _, msg := setupIdentity(t)
	alice := sdk.AccAddress([]byte("alice_______________")).String()
	bob := sdk.AccAddress([]byte("bob_________________")).String()

	_, err := msg.RegisterIdentity(ctx, reg(alice, "alice", []byte("bio-alice")))
	require.NoError(t, err)

	_, err = msg.RegisterIdentity(ctx, reg(bob, "alice", []byte("bio-bob")))
	require.ErrorIs(t, err, types.ErrIdentityExists)

	_, err = msg.RegisterIdentity(ctx, reg(bob, "bob", []byte("bio-alice")))
	require.ErrorIs(t, err, types.ErrUniquenessUsed)
}

// An issuer attestation nonce is single-use.
func TestRegisterIdentity_NonceIsSingleUse(t *testing.T) {
	ctx, _, msg := setupIdentity(t)
	alice := sdk.AccAddress([]byte("alice_______________")).String()
	bob := sdk.AccAddress([]byte("bob_________________")).String()

	first := reg(alice, "alice", []byte("bio-alice"))
	_, err := msg.RegisterIdentity(ctx, first)
	require.NoError(t, err)

	replay := reg(bob, "bob", []byte("bio-bob"))
	replay.Nonce = first.Nonce
	_, err = msg.RegisterIdentity(ctx, replay)
	require.ErrorIs(t, err, types.ErrNonceReused)

	replay.Nonce = []byte("nonce-bob-fresh")
	_, err = msg.RegisterIdentity(ctx, replay)
	require.NoError(t, err)
}

func TestBootstrapLatch_OneWay(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	p := k.GetParams(ctx)
	p.BootstrapThreshold = 2
	require.NoError(t, k.SetParams(ctx, p))
	require.True(t, k.BootstrapPhase(ctx))

	_, err := msg.RegisterIdentity(ctx, reg(sdk.AccAddress([]byte("u1__________________")).String(), "1", []byte("b1")))
	require.NoError(t, err)
	require.True(t, k.BootstrapPhase(ctx), "still in bootstrap after 1 identity")

	_, err = msg.RegisterIdentity(ctx, reg(sdk.AccAddress([]byte("u2__________________")).String(), "2", []byte("b2")))
	require.NoError(t, err)
	require.False(t, k.BootstrapPhase(ctx), "crossing the threshold closes bootstrap one-way")
	require.Equal(t, uint64(2), k.GetIdentityCount(ctx))
}

func TestEligibility_RespectsMinAge(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	now := time.Unix(10_000_000, 0)
	ctx = ctx.WithBlockTime(now)
	ctrl := sdk.AccAddress([]byte("voter_______________")).String()

	_, err := msg.RegisterIdentity(ctx, reg(ctrl, "voter", []byte("b-voter")))
	require.NoError(t, err)

	minAge := k.MinIdentityAge(ctx)
	require.False(t, k.IsEligibleControllerAt(ctx, ctrl, now, minAge))
	require.True(t, k.IsEligibleControllerAt(ctx, ctrl, now.Add(8*24*time.Hour), minAge))
	require.Equal(t, uint64(1), k.CountEligibleControllersAt(ctx, now.Add(8*24*time.Hour), minAge))
}

// The quorum denominator counts DISTINCT controllers, not DIDs — so multiple DIDs under one controller cannot inflate the quorum denominator (which would suppress turnout, since the tally dedups votes per controller).
func TestQuorumDenominator_CountsControllersNotDIDs(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	now := time.Unix(10_000_000, 0)
	ctx = ctx.WithBlockTime(now)
	ctrl := sdk.AccAddress([]byte("multi-did-controller")).String()

	_, err := msg.RegisterIdentity(ctx, reg(ctrl, "did-a", []byte("bio-a")))
	require.NoError(t, err)
	_, err = msg.RegisterIdentity(ctx, reg(ctrl, "did-b", []byte("bio-b")))
	require.NoError(t, err)

	minAge := k.MinIdentityAge(ctx)
	at := now.Add(8 * 24 * time.Hour)
	require.Equal(t, uint64(2), k.GetIdentityCount(ctx), "both DIDs are registered")
	require.Equal(t, uint64(1), k.CountEligibleControllersAt(ctx, at, minAge),
		"two DIDs under one controller must count once toward quorum")
}

// TestCountEligible_MatchesReferenceScan pins the memoized CountEligibleControllersAt to the original O(N) distinct-eligible-controller scan across many cutoffs and a varied registry (multiple DIDs per controller, revoked DIDs, mixed ages).
func TestCountEligible_MatchesReferenceScan(t *testing.T) {
	ctx, k, _ := setupIdentity(t)
	ctx = ctx.WithExecMode(sdk.ExecModeFinalize) // exercise the memoized finalize path
	seed := []types.DIDDocument{
		{Did: "did:phi:a1", Controller: "ctrlA", Status: types.DID_STATUS_ACTIVE, CreatedAt: 100, PubKey: []byte("a1")},
		{Did: "did:phi:a2", Controller: "ctrlA", Status: types.DID_STATUS_ACTIVE, CreatedAt: 250, PubKey: []byte("a2")},
		{Did: "did:phi:b1", Controller: "ctrlB", Status: types.DID_STATUS_REVOKED, CreatedAt: 50, PubKey: []byte("b1")},
		{Did: "did:phi:b2", Controller: "ctrlB", Status: types.DID_STATUS_ACTIVE, CreatedAt: 300, PubKey: []byte("b2")},
		{Did: "did:phi:c1", Controller: "ctrlC", Status: types.DID_STATUS_REVOKED, CreatedAt: 10, PubKey: []byte("c1")},
		{Did: "did:phi:e1", Controller: "ctrlE", Status: types.DID_STATUS_ACTIVE, CreatedAt: 500, PubKey: []byte("e1")},
	}
	for _, d := range seed {
		k.SetIdentity(ctx, d)
	}
	ref := func(cutoff int64) uint64 {
		set := map[string]struct{}{}
		k.IterateIdentities(ctx, func(d types.DIDDocument) bool {
			if d.Status == types.DID_STATUS_ACTIVE && d.CreatedAt <= cutoff {
				set[d.Controller] = struct{}{}
			}
			return false
		})
		return uint64(len(set))
	}
	for _, cutoff := range []int64{0, 49, 50, 99, 100, 101, 249, 300, 499, 500, 1000} {
		at := time.Unix(cutoff, 0)
		require.Equal(t, ref(cutoff), k.CountEligibleControllersAt(ctx, at, 0), "cutoff=%d", cutoff)
	}
}

// TestCountEligible_CacheInvalidatedOnWrite proves the memoization is clear-on-write: a count, then an identity write, then a recount must reflect the write (a stale snapshot would corrupt the quorum denominator and fork consensus).
func TestCountEligible_CacheInvalidatedOnWrite(t *testing.T) {
	ctx, k, _ := setupIdentity(t)
	ctx = ctx.WithExecMode(sdk.ExecModeFinalize) // exercise the memoized finalize path
	at := time.Unix(1000, 0)
	k.SetIdentity(ctx, types.DIDDocument{Did: "did:phi:x1", Controller: "ctrlX", Status: types.DID_STATUS_ACTIVE, CreatedAt: 100, PubKey: []byte("x1")})
	require.Equal(t, uint64(1), k.CountEligibleControllersAt(ctx, at, 0)) // builds + caches {ctrlX}

	k.SetIdentity(ctx, types.DIDDocument{Did: "did:phi:y1", Controller: "ctrlY", Status: types.DID_STATUS_ACTIVE, CreatedAt: 150, PubKey: []byte("y1")})
	require.Equal(t, uint64(2), k.CountEligibleControllersAt(ctx, at, 0), "cache must invalidate on identity write")

	k.SetIdentity(ctx, types.DIDDocument{Did: "did:phi:x1", Controller: "ctrlX", Status: types.DID_STATUS_REVOKED, CreatedAt: 100, PubKey: []byte("x1")})
	require.Equal(t, uint64(1), k.CountEligibleControllersAt(ctx, at, 0), "revocation must be reflected")
}

// TestCountEligible_NonFinalizeComputesFresh proves the cross-context hardening: a non-finalize read (CheckTx/query) returns a correct, freshly computed count and never serves a stale shared cache, so it can never feed the finalize-path quorum denominator a wrong value.
func TestCountEligible_NonFinalizeComputesFresh(t *testing.T) {
	ctx, k, _ := setupIdentity(t)
	at := time.Unix(1000, 0)
	fin := ctx.WithExecMode(sdk.ExecModeFinalize)
	chk := ctx.WithExecMode(sdk.ExecModeCheck)

	k.SetIdentity(fin, types.DIDDocument{Did: "did:phi:p1", Controller: "ctrlP", Status: types.DID_STATUS_ACTIVE, CreatedAt: 100, PubKey: []byte("p1")})
	require.Equal(t, uint64(1), k.CountEligibleControllersAt(fin, at, 0)) // builds + caches in finalize

	k.SetIdentity(fin, types.DIDDocument{Did: "did:phi:q1", Controller: "ctrlQ", Status: types.DID_STATUS_ACTIVE, CreatedAt: 150, PubKey: []byte("q1")})
	require.Equal(t, uint64(2), k.CountEligibleControllersAt(chk, at, 0), "check-mode read must compute fresh")
	require.Equal(t, uint64(2), k.CountEligibleControllersAt(fin, at, 0), "finalize read agrees")
}

// Identity genesis Validate must mirror the runtime's *immutable* identity invariants (pub_key length, valid bech32 controller, known status, and a present/unique uniqueness marker) so a curated/compromised genesis cannot seed controller-spoofed/malformed DIDs the tally would treat as eligible humans — but it must NOT require pubkey↔DID self-certification, because RotateIdentityKey keeps the DID stable while replacing pub_key (a rotated identity is non-self-certifying by design).
func TestIdentityGenesisValidate_MirrorsRuntimeChecks(t *testing.T) {
	ctrl := sdk.AccAddress([]byte("genesis-controller__")).String()
	valid := types.DIDDocument{
		Did: didFor("g1"), PubKey: pubFor("g1"), Controller: ctrl,
		Status: types.DID_STATUS_ACTIVE, UniquenessHash: []byte("uniq-g1"), CreatedAt: 1,
	}
	gen := func(d types.DIDDocument) types.GenesisState {
		return types.GenesisState{Params: types.DefaultParams(), Identities: []types.DIDDocument{d}, IdentityCount: 1}
	}
	require.NoError(t, gen(valid).Validate(), "a self-certifying genesis identity is valid")

	d := valid
	d.Did = didFor("other")
	selfCert, err := types.DeriveDIDFromP256(d.PubKey)
	require.NoError(t, err)
	require.NotEqual(t, d.Did, selfCert, "precondition: this models a rotated (non-self-certifying) DID")
	require.NoError(t, gen(d).Validate(), "a rotated (non-self-certifying) DID must be accepted")

	d = valid
	d.PubKey = nil
	require.Error(t, gen(d).Validate(), "empty pub_key must be rejected")

	d = valid
	d.Controller = "not-a-bech32-address"
	require.Error(t, gen(d).Validate(), "invalid controller must be rejected")

	d = valid
	d.Status = types.DID_STATUS_UNSPECIFIED
	require.Error(t, gen(d).Validate(), "unspecified status must be rejected")
}

// ExportGenesis must carry the issuer single-use nonce markers and the validator↔DID bindings, and InitGenesis must restore them — otherwise an export→import would reset issuer-nonce anti-replay and drop the unique-DID-per-validator bindings.
func TestGenesis_MarkerRoundTrip(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	ctx = ctx.WithBlockHeight(1)
	ctrl := sdk.AccAddress([]byte("rt-controller_______")).String()
	_, err := msg.RegisterIdentity(ctx, reg(ctrl, "rt", []byte("bio-rt"))) // consumes an issuer nonce
	require.NoError(t, err)
	rtValoper := sdk.ValAddress([]byte("roundtrip-operator__")).String()
	k.BindValidatorToDID(ctx, didFor("rt"), rtValoper)

	gs := k.ExportGenesis(ctx)
	var sawNonce, sawBinding bool
	for _, e := range gs.StoreEntries {
		if bytes.HasPrefix(e.Key, types.IssuerNoncePrefix) {
			sawNonce = true
		}
		if bytes.HasPrefix(e.Key, types.DIDToValidatorPrefix) || bytes.HasPrefix(e.Key, types.ValidatorToDIDPrefix) {
			sawBinding = true
		}
	}
	require.True(t, sawNonce, "issuer nonce marker must be exported")
	require.True(t, sawBinding, "validator↔DID binding must be exported")
	require.NoError(t, gs.Validate())

	ctx2, k2, _ := setupIdentity(t)
	k2.InitGenesis(ctx2, *gs)
	bound, ok := k2.ValidatorForDID(ctx2, didFor("rt"))
	require.True(t, ok, "validator↔DID binding must survive export→import")
	require.Equal(t, rtValoper, bound)

	bad := *gs
	bad.StoreEntries = append([]types.StoreEntry{{Key: append(append([]byte{}, types.DIDPrefix...), 'x'), Value: []byte("x")}}, gs.StoreEntries...)
	require.Error(t, bad.Validate(), "a store_entry outside the marker prefixes must be rejected")
}

// PrimaryDID resolves a controller to its ACTIVE DID through the (controller ‖ did) index — the resolution the institutions module's network-wide per-DID redeem cap keys its counter on.
func TestPrimaryDID_ResolvesActiveDIDAndReportsMissesHonestly(t *testing.T) {
	ctx, k, _ := setupIdentity(t)

	k.SetIdentity(ctx, types.DIDDocument{
		Did: "did:phi:alice", Controller: "ctrlAlice", Status: types.DID_STATUS_ACTIVE,
		CreatedAt: 100, PubKey: []byte("a"),
	})
	k.SetIdentity(ctx, types.DIDDocument{
		Did: "did:phi:mallory", Controller: "ctrlMallory", Status: types.DID_STATUS_REVOKED,
		CreatedAt: 100, PubKey: []byte("m"),
	})

	did, ok := k.PrimaryDID(ctx, "ctrlAlice")
	require.True(t, ok, "an ACTIVE DID must resolve")
	require.Equal(t, "did:phi:alice", did)

	_, ok = k.PrimaryDID(ctx, "ctrlNobody")
	require.False(t, ok, "a controller with no DID must not resolve")

	_, ok = k.PrimaryDID(ctx, "ctrlMallory")
	require.False(t, ok, "a non-ACTIVE DID must not resolve")

	again, ok := k.PrimaryDID(ctx, "ctrlAlice")
	require.True(t, ok)
	require.Equal(t, did, again)
}
