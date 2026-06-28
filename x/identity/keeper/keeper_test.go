// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"bytes"
	"testing"
	"time"

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

// setupIdentityV injects a chosen verifier and seeds a trusted issuer (so registration can succeed).
func setupIdentityV(t *testing.T, v phicrypto.Verifier) (sdk.Context, keeper.Keeper, types.MsgServer) {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_id"))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	authority := sdk.AccAddress([]byte("gov_authority_______")).String()
	k := keeper.NewKeeper(cdc, key, authority, v)
	require.NoError(t, k.SetParams(testCtx.Ctx, types.DefaultParams()))
	k.SetTrustedIssuer(testCtx.Ctx, types.TrustedIssuer{Did: testIssuerDID, PubKey: []byte("issuer-pk"), Active: true})
	return testCtx.Ctx, k, keeper.NewMsgServerImpl(k)
}

// setupIdentity is the common happy-path harness (AcceptAll verifier + a seeded trusted issuer).
func setupIdentity(t *testing.T) (sdk.Context, keeper.Keeper, types.MsgServer) {
	return setupIdentityV(t, phicrypto.AcceptAll())
}

// pubFor/didFor derive a distinct P-256 key and its canonical (self-certifying) DID per label.
func pubFor(label string) []byte { return []byte("pubkey-" + label) }
func didFor(label string) string { return types.DeriveDIDFromP256(pubFor(label)) }

// reg builds a registration whose DID is the canonical derivation of a per-label key, attested by the
// seeded trusted issuer with a (test-accepted) proof-of-possession.
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

	// Duplicate DID (same label → same self-certifying DID).
	_, err = msg.RegisterIdentity(ctx, reg(bob, "alice", []byte("bio-bob")))
	require.ErrorIs(t, err, types.ErrIdentityExists)

	// Duplicate uniqueness marker (one-human-one-DID).
	_, err = msg.RegisterIdentity(ctx, reg(bob, "bob", []byte("bio-alice")))
	require.ErrorIs(t, err, types.ErrUniquenessUsed)
}

// An issuer attestation nonce is single-use. Reusing the same (issuer, nonce) on a second,
// otherwise-valid registration is rejected as a replay; a fresh nonce then succeeds.
func TestRegisterIdentity_NonceIsSingleUse(t *testing.T) {
	ctx, _, msg := setupIdentity(t)
	alice := sdk.AccAddress([]byte("alice_______________")).String()
	bob := sdk.AccAddress([]byte("bob_________________")).String()

	first := reg(alice, "alice", []byte("bio-alice"))
	_, err := msg.RegisterIdentity(ctx, first)
	require.NoError(t, err)

	// A fresh, otherwise-valid registration (new DID + new uniqueness) that reuses alice's nonce.
	replay := reg(bob, "bob", []byte("bio-bob"))
	replay.Nonce = first.Nonce
	_, err = msg.RegisterIdentity(ctx, replay)
	require.ErrorIs(t, err, types.ErrNonceReused)

	// The nonce was the only problem: a fresh nonce lets the same new identity register.
	replay.Nonce = []byte("nonce-bob-fresh")
	_, err = msg.RegisterIdentity(ctx, replay)
	require.NoError(t, err)
}

func TestBootstrapLatch_OneWay(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	// Small threshold for the test.
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
	// min_identity_age = 7 days.
	now := time.Unix(10_000_000, 0)
	ctx = ctx.WithBlockTime(now)
	ctrl := sdk.AccAddress([]byte("voter_______________")).String()

	_, err := msg.RegisterIdentity(ctx, reg(ctrl, "voter", []byte("b-voter")))
	require.NoError(t, err)

	minAge := k.MinIdentityAge(ctx)
	// Immediately after registration: not eligible (age < min_age).
	require.False(t, k.IsEligibleControllerAt(ctx, ctrl, now, minAge))
	// 8 days later: eligible.
	require.True(t, k.IsEligibleControllerAt(ctx, ctrl, now.Add(8*24*time.Hour), minAge))
	require.Equal(t, uint64(1), k.CountEligibleControllersAt(ctx, now.Add(8*24*time.Hour), minAge))
}

// The quorum denominator counts DISTINCT controllers, not DIDs — so multiple DIDs under one
// controller cannot inflate the quorum denominator (which would suppress turnout, since the tally
// dedups votes per controller).
func TestQuorumDenominator_CountsControllersNotDIDs(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	now := time.Unix(10_000_000, 0)
	ctx = ctx.WithBlockTime(now)
	ctrl := sdk.AccAddress([]byte("multi-did-controller")).String()

	// One controller registers TWO distinct DIDs (distinct keys + uniqueness markers).
	_, err := msg.RegisterIdentity(ctx, reg(ctrl, "did-a", []byte("bio-a")))
	require.NoError(t, err)
	_, err = msg.RegisterIdentity(ctx, reg(ctrl, "did-b", []byte("bio-b")))
	require.NoError(t, err)

	minAge := k.MinIdentityAge(ctx)
	at := now.Add(8 * 24 * time.Hour)
	require.Equal(t, uint64(2), k.GetIdentityCount(ctx), "both DIDs are registered")
	// But the eligible-controller count — the quorum denominator — is 1.
	require.Equal(t, uint64(1), k.CountEligibleControllersAt(ctx, at, minAge),
		"two DIDs under one controller must count once toward quorum")
}

// TestCountEligible_MatchesReferenceScan pins the memoized CountEligibleControllersAt to
// the original O(N) distinct-eligible-controller scan across many cutoffs and a varied registry
// (multiple DIDs per controller, revoked DIDs, mixed ages). The two must agree exactly — the quorum
// denominator is consensus-critical, so the optimization must be behaviour-preserving.
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
	// Reference: the original distinct-eligible-controller scan.
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
	// minAge=0 makes cutoff == t, so each timestamp is exercised directly.
	for _, cutoff := range []int64{0, 49, 50, 99, 100, 101, 249, 300, 499, 500, 1000} {
		at := time.Unix(cutoff, 0)
		require.Equal(t, ref(cutoff), k.CountEligibleControllersAt(ctx, at, 0), "cutoff=%d", cutoff)
	}
}

// TestCountEligible_CacheInvalidatedOnWrite proves the memoization is clear-on-write: a count, then an
// identity write, then a recount must reflect the write (a stale snapshot would corrupt the quorum
// denominator and fork consensus).
func TestCountEligible_CacheInvalidatedOnWrite(t *testing.T) {
	ctx, k, _ := setupIdentity(t)
	ctx = ctx.WithExecMode(sdk.ExecModeFinalize) // exercise the memoized finalize path
	at := time.Unix(1000, 0)
	k.SetIdentity(ctx, types.DIDDocument{Did: "did:phi:x1", Controller: "ctrlX", Status: types.DID_STATUS_ACTIVE, CreatedAt: 100, PubKey: []byte("x1")})
	require.Equal(t, uint64(1), k.CountEligibleControllersAt(ctx, at, 0)) // builds + caches {ctrlX}

	// A new controller after the first count: the recount must refresh.
	k.SetIdentity(ctx, types.DIDDocument{Did: "did:phi:y1", Controller: "ctrlY", Status: types.DID_STATUS_ACTIVE, CreatedAt: 150, PubKey: []byte("y1")})
	require.Equal(t, uint64(2), k.CountEligibleControllersAt(ctx, at, 0), "cache must invalidate on identity write")

	// Revoking ctrlX's only DID drops it from the denominator.
	k.SetIdentity(ctx, types.DIDDocument{Did: "did:phi:x1", Controller: "ctrlX", Status: types.DID_STATUS_REVOKED, CreatedAt: 100, PubKey: []byte("x1")})
	require.Equal(t, uint64(1), k.CountEligibleControllersAt(ctx, at, 0), "revocation must be reflected")
}

// TestCountEligible_NonFinalizeComputesFresh proves the cross-context hardening: a
// non-finalize read (CheckTx/query) returns a correct, freshly computed count and never serves a stale
// shared cache, so it can never feed the finalize-path quorum denominator a wrong value.
func TestCountEligible_NonFinalizeComputesFresh(t *testing.T) {
	ctx, k, _ := setupIdentity(t)
	at := time.Unix(1000, 0)
	fin := ctx.WithExecMode(sdk.ExecModeFinalize)
	chk := ctx.WithExecMode(sdk.ExecModeCheck)

	k.SetIdentity(fin, types.DIDDocument{Did: "did:phi:p1", Controller: "ctrlP", Status: types.DID_STATUS_ACTIVE, CreatedAt: 100, PubKey: []byte("p1")})
	require.Equal(t, uint64(1), k.CountEligibleControllersAt(fin, at, 0)) // builds + caches in finalize

	// A second controller added in finalize; a CheckTx read must reflect current state (fresh compute).
	k.SetIdentity(fin, types.DIDDocument{Did: "did:phi:q1", Controller: "ctrlQ", Status: types.DID_STATUS_ACTIVE, CreatedAt: 150, PubKey: []byte("q1")})
	require.Equal(t, uint64(2), k.CountEligibleControllersAt(chk, at, 0), "check-mode read must compute fresh")
	require.Equal(t, uint64(2), k.CountEligibleControllersAt(fin, at, 0), "finalize read agrees")
}

// Identity genesis Validate must mirror the runtime's *immutable* identity invariants (pub_key
// length, valid bech32 controller, known status, and a present/unique uniqueness marker) so a
// curated/compromised genesis cannot seed controller-spoofed/malformed DIDs the tally would treat as
// eligible humans — but it must NOT require pubkey↔DID self-certification, because RotateIdentityKey
// keeps the DID stable while replacing pub_key (a rotated identity is non-self-certifying by design).
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

	// A rotated identity (did != DeriveDIDFromP256(pub_key)) must be ACCEPTED — RotateIdentityKey
	// preserves the DID while replacing pub_key, so genesis must not reject the non-self-certifying result.
	d := valid
	d.Did = didFor("other")
	require.NotEqual(t, d.Did, types.DeriveDIDFromP256(d.PubKey), "precondition: this models a rotated (non-self-certifying) DID")
	require.NoError(t, gen(d).Validate(), "a rotated (non-self-certifying) DID must be accepted")

	// Empty / oversized pub_key.
	d = valid
	d.PubKey = nil
	require.Error(t, gen(d).Validate(), "empty pub_key must be rejected")

	// Controller is not a valid bech32 address (controller spoofing).
	d = valid
	d.Controller = "not-a-bech32-address"
	require.Error(t, gen(d).Validate(), "invalid controller must be rejected")

	// Unknown status enum.
	d = valid
	d.Status = types.DID_STATUS_UNSPECIFIED
	require.Error(t, gen(d).Validate(), "unspecified status must be rejected")
}

// ExportGenesis must carry the issuer single-use nonce markers and the validator↔DID
// bindings, and InitGenesis must restore them — otherwise an export→import would reset issuer-nonce
// anti-replay and drop the unique-DID-per-validator bindings.
func TestGenesis_MarkerRoundTrip(t *testing.T) {
	ctx, k, msg := setupIdentity(t)
	ctx = ctx.WithBlockHeight(1)
	ctrl := sdk.AccAddress([]byte("rt-controller_______")).String()
	_, err := msg.RegisterIdentity(ctx, reg(ctrl, "rt", []byte("bio-rt"))) // consumes an issuer nonce
	require.NoError(t, err)
	k.BindValidatorToDID(ctx, didFor("rt"), "phivaloperrt")

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

	// Re-import into a fresh keeper: the binding (and nonce) survive verbatim.
	ctx2, k2, _ := setupIdentity(t)
	k2.InitGenesis(ctx2, *gs)
	bound, ok := k2.ValidatorForDID(ctx2, didFor("rt"))
	require.True(t, ok, "validator↔DID binding must survive export→import")
	require.Equal(t, "phivaloperrt", bound)

	// A StoreEntry whose key escapes the allowed marker prefixes is rejected (cannot overwrite a DID).
	bad := *gs
	bad.StoreEntries = append([]types.StoreEntry{{Key: append(append([]byte{}, types.DIDPrefix...), 'x'), Value: []byte("x")}}, gs.StoreEntries...)
	require.Error(t, bad.Validate(), "a store_entry outside the marker prefixes must be rejected")
}
