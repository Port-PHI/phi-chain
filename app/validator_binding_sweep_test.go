// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	slashingkeeper "github.com/cosmos/cosmos-sdk/x/slashing/keeper"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/app"
	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	identitykeeper "github.com/Port-PHI/phi-chain/x/identity/keeper"
	identitytypes "github.com/Port-PHI/phi-chain/x/identity/types"
	insttypes "github.com/Port-PHI/phi-chain/x/institutions/types"
)

type sweepFixture struct {
	a        *app.App
	ctx      sdk.Context
	valAddr  sdk.ValAddress
	consAddr sdk.ConsAddress
	acct     sdk.AccAddress
	did      string
}

func newSweepFixture(t *testing.T, label string) *sweepFixture {
	t.Helper()
	a := newTestApp(t)
	blockTime := time.Unix(1_700_000_000, 0).UTC()
	ctx := a.NewUncachedContext(false, cmtproto.Header{Height: 10, Time: blockTime})

	sp := stakingtypes.DefaultParams()
	sp.BondDenom = cointypes.Denom
	require.NoError(t, a.StakingKeeper.SetParams(ctx, sp))
	require.NoError(t, a.SlashingKeeper.SetParams(ctx, slashingtypes.DefaultParams()))
	require.NoError(t, a.DistrKeeper.FeePool.Set(ctx, distrtypes.InitialFeePool()))

	acct := sdk.AccAddress([]byte(fmt.Sprintf("%-20s", label)[:20]))
	valAddr := sdk.ValAddress(acct)

	phi := math.NewIntFromUint64(cointypes.UphiPerPhi)
	stake := phi.MulRaw(100)
	require.NoError(t, a.BankKeeper.MintCoins(ctx, insttypes.ModuleName, cointypes.CoinsOf(stake)))
	require.NoError(t, a.BankKeeper.SendCoinsFromModuleToAccount(ctx, insttypes.ModuleName, acct, cointypes.CoinsOf(stake)))

	consPub := ed25519.GenPrivKeyFromSecret([]byte(label + "-cons")).PubKey()
	val, err := stakingtypes.NewValidator(valAddr.String(), consPub, stakingtypes.Description{Moniker: label})
	require.NoError(t, err)
	val.Status = stakingtypes.Unbonded
	require.NoError(t, a.StakingKeeper.SetValidator(ctx, val))
	require.NoError(t, a.StakingKeeper.SetValidatorByConsAddr(ctx, val))
	require.NoError(t, a.StakingKeeper.SetNewValidatorByPowerIndex(ctx, val))
	require.NoError(t, a.DistrKeeper.Hooks().AfterValidatorCreated(ctx, valAddr))
	_, err = a.StakingKeeper.Delegate(ctx, acct, stake, stakingtypes.Unbonded, val, true)
	require.NoError(t, err)
	_, err = a.StakingKeeper.EndBlocker(ctx)
	require.NoError(t, err)

	consAddr := sdk.ConsAddress(consPub.Address())
	require.NoError(t, a.SlashingKeeper.AddPubkey(ctx, consPub))
	require.NoError(t, a.SlashingKeeper.SetValidatorSigningInfo(ctx, consAddr,
		slashingtypes.NewValidatorSigningInfo(consAddr, ctx.BlockHeight(), 0, time.Time{}, false, 0)))

	return &sweepFixture{a: a, ctx: ctx, valAddr: valAddr, consAddr: consAddr, acct: acct}
}

func (f *sweepFixture) registerDID(t *testing.T, status identitytypes.DIDStatus, bind bool) {
	t.Helper()
	f.did = "did:phi:" + f.acct.String()
	f.a.IdentityKeeper.SetIdentity(f.ctx, identitytypes.DIDDocument{
		Did:            f.did,
		Controller:     f.acct.String(),
		PubKey:         []byte("pk"),
		UniquenessHash: []byte("uniq-" + f.acct.String()),
		Status:         status,
		CreatedAt:      f.ctx.BlockTime().Unix() - 1_000_000,
	})
	if bind {
		f.a.IdentityKeeper.BindValidatorToDID(f.ctx, f.did, f.valAddr.String())
	}
}

func (f *sweepFixture) setStatusWithoutHooks(t *testing.T, status identitytypes.DIDStatus) {
	t.Helper()
	doc, found := f.a.IdentityKeeper.GetIdentity(f.ctx, f.did)
	require.True(t, found)
	doc.Status = status
	f.a.IdentityKeeper.SetIdentity(f.ctx, doc)
}

func (f *sweepFixture) sweep(t *testing.T) map[string]identitykeeper.ValidatorSweepOutcome {
	t.Helper()
	out, err := f.a.IdentityKeeper.SweepValidatorBindings(f.ctx, f.a.StakingKeeper, f.a.SlashingKeeper)
	require.NoError(t, err)
	return out
}

func (f *sweepFixture) inActiveSet(t *testing.T) bool {
	t.Helper()
	val, err := f.a.StakingKeeper.GetValidator(f.ctx, f.valAddr)
	require.NoError(t, err)
	return !val.IsJailed() && val.IsBonded()
}

func (f *sweepFixture) tombstoned() bool {
	return f.a.SlashingKeeper.IsTombstoned(f.ctx, f.consAddr)
}

func (f *sweepFixture) tokens(t *testing.T) math.Int {
	t.Helper()
	val, err := f.a.StakingKeeper.GetValidator(f.ctx, f.valAddr)
	require.NoError(t, err)
	return val.Tokens
}

type bindingCase struct {
	name   string
	status identitytypes.DIDStatus
	hasDID bool
	bound  bool

	wantOutcome    identitykeeper.ValidatorSweepOutcome
	wantInActive   bool
	wantTombstoned bool
}

func bindingCases() []bindingCase {
	return []bindingCase{
		{
			name:   "bound + ACTIVE keeps validating",
			status: identitytypes.DID_STATUS_ACTIVE, hasDID: true, bound: true,
			wantOutcome: identitykeeper.SweepKept, wantInActive: true,
		},
		{
			name:   "bound + SUSPENDED is jailed but not tombstoned",
			status: identitytypes.DID_STATUS_SUSPENDED, hasDID: true, bound: true,
			wantOutcome: identitykeeper.SweepJailed, wantInActive: false, wantTombstoned: false,
		},
		{
			name:   "bound + REVOKED is jailed and tombstoned",
			status: identitytypes.DID_STATUS_REVOKED, hasDID: true, bound: true,
			wantOutcome: identitykeeper.SweepTombstoned, wantInActive: false, wantTombstoned: true,
		},
		{
			name:   "unbound but the account holds an ACTIVE DID is adopted",
			status: identitytypes.DID_STATUS_ACTIVE, hasDID: true, bound: false,
			wantOutcome: identitykeeper.SweepBound, wantInActive: true,
		},
		{
			name:   "unbound and the account's DID is SUSPENDED is jailed, not tombstoned",
			status: identitytypes.DID_STATUS_SUSPENDED, hasDID: true, bound: false,
			wantOutcome: identitykeeper.SweepJailed, wantInActive: false, wantTombstoned: false,
		},
		{
			name:   "unbound and the account's DID is REVOKED is jailed and tombstoned",
			status: identitytypes.DID_STATUS_REVOKED, hasDID: true, bound: false,
			wantOutcome: identitykeeper.SweepTombstoned, wantInActive: false, wantTombstoned: true,
		},
		{
			name:   "no identity at all is jailed, not tombstoned",
			hasDID: false, bound: false,
			wantOutcome: identitykeeper.SweepJailed, wantInActive: false, wantTombstoned: false,
		},
	}
}

// TestValidatorSweep_EveryDIDStatusAndBindingState walks the full matrix and asserts active-set membership after one sweep in every cell.
func TestValidatorSweep_EveryDIDStatusAndBindingState(t *testing.T) {
	for i, tc := range bindingCases() {
		t.Run(tc.name, func(t *testing.T) {
			f := newSweepFixture(t, fmt.Sprintf("sweep-val-%d", i))
			require.True(t, f.inActiveSet(t), "precondition: the validator is validating")
			stakeBefore := f.tokens(t)

			if tc.hasDID {
				f.registerDID(t, tc.status, tc.bound)
			}

			outcomes := f.sweep(t)
			require.Equal(t, tc.wantOutcome, outcomes[f.valAddr.String()])
			require.Equal(t, tc.wantInActive, f.inActiveSet(t),
				"active-set membership after the sweep")
			require.Equal(t, tc.wantTombstoned, f.tombstoned(),
				"only a TERMINAL identity failure may tombstone")

			require.Equal(t, stakeBefore.String(), f.tokens(t).String(),
				"the sweep must never slash")
		})
	}
}

// THE NO-HOOK-RELIANCE PROOF.
func TestValidatorSweep_DoesNotRelyOnAnyStatusChangeHook(t *testing.T) {
	for _, tc := range []struct {
		name           string
		status         identitytypes.DIDStatus
		wantTombstoned bool
	}{
		{name: "revoked mid-life", status: identitytypes.DID_STATUS_REVOKED, wantTombstoned: true},
		{name: "suspended mid-life", status: identitytypes.DID_STATUS_SUSPENDED, wantTombstoned: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSweepFixture(t, "nohook-"+tc.name[:6])
			f.registerDID(t, identitytypes.DID_STATUS_ACTIVE, true)

			f.sweep(t)
			require.True(t, f.inActiveSet(t))

			f.setStatusWithoutHooks(t, tc.status)
			require.True(t, f.inActiveSet(t),
				"no hook fired, so nothing has removed the validator yet — this is the gap")

			f.sweep(t)
			require.False(t, f.inActiveSet(t),
				"the sweep alone must remove a validator whose identity is no longer ACTIVE")
			require.Equal(t, tc.wantTombstoned, f.tombstoned())
		})
	}
}

// SUSPENDED is reversible, and the reversal has to actually work: once the DID is ACTIVE again the validator may return through the ordinary staking unjail path.
func TestValidatorSweep_SuspensionIsReversibleAndRevocationIsNot(t *testing.T) {
	t.Run("suspended then reinstated can unjail", func(t *testing.T) {
		f := newSweepFixture(t, "reinstate-val______")
		f.registerDID(t, identitytypes.DID_STATUS_ACTIVE, true)

		f.setStatusWithoutHooks(t, identitytypes.DID_STATUS_SUSPENDED)
		f.sweep(t)
		require.False(t, f.inActiveSet(t))
		require.False(t, f.tombstoned(), "a reversible freeze must not tombstone")

		f.setStatusWithoutHooks(t, identitytypes.DID_STATUS_ACTIVE)
		require.Equal(t, identitykeeper.SweepKept, f.sweep(t)[f.valAddr.String()])

		msg := slashingkeeper.NewMsgServerImpl(f.a.SlashingKeeper)
		_, err := msg.Unjail(f.ctx, &slashingtypes.MsgUnjail{ValidatorAddr: f.valAddr.String()})
		require.NoError(t, err, "a reinstated identity must be able to unjail")
		require.True(t, f.inActiveSetAfterStakingEndBlock(t),
			"and rejoin the active set")
	})

	t.Run("revoked cannot unjail, ever", func(t *testing.T) {
		f := newSweepFixture(t, "revoked-val_______")
		f.registerDID(t, identitytypes.DID_STATUS_ACTIVE, true)

		f.setStatusWithoutHooks(t, identitytypes.DID_STATUS_REVOKED)
		f.sweep(t)
		require.False(t, f.inActiveSet(t))
		require.True(t, f.tombstoned(), "revocation is terminal, so the removal must be too")

		msg := slashingkeeper.NewMsgServerImpl(f.a.SlashingKeeper)
		_, err := msg.Unjail(f.ctx, &slashingtypes.MsgUnjail{ValidatorAddr: f.valAddr.String()})
		require.Error(t, err, "a tombstoned validator must never be able to unjail")

		f.setStatusWithoutHooks(t, identitytypes.DID_STATUS_ACTIVE)
		f.sweep(t)
		_, err = msg.Unjail(f.ctx, &slashingtypes.MsgUnjail{ValidatorAddr: f.valAddr.String()})
		require.Error(t, err, "reinstating a revoked identity must not resurrect the validator")
	})
}

func (f *sweepFixture) inActiveSetAfterStakingEndBlock(t *testing.T) bool {
	t.Helper()
	_, err := f.a.StakingKeeper.EndBlocker(f.ctx)
	require.NoError(t, err)
	return f.inActiveSet(t)
}

// The sweep is DETERMINISTIC and its cost is bounded by the validator set, never by the identity registry.
func TestValidatorSweep_IsDeterministicAndBoundedByTheValidatorSet(t *testing.T) {
	f := newSweepFixture(t, "determinism-val____")
	f.registerDID(t, identitytypes.DID_STATUS_ACTIVE, true)

	for i := 0; i < 5_000; i++ {
		f.a.IdentityKeeper.SetIdentity(f.ctx, identitytypes.DIDDocument{
			Did:            fmt.Sprintf("did:phi:filler-%06d", i),
			Controller:     sdk.AccAddress([]byte(fmt.Sprintf("filler-acct-%08d", i))).String(),
			PubKey:         []byte("pk"),
			UniquenessHash: []byte(fmt.Sprintf("uniq-filler-%06d", i)),
			Status:         identitytypes.DID_STATUS_ACTIVE,
			CreatedAt:      1,
		})
	}

	first := f.sweep(t)
	for i := 0; i < 5; i++ {
		require.Equal(t, first, f.sweep(t), "the sweep must be a pure function of committed state")
	}

	before := f.ctx.GasMeter().GasConsumed()
	f.sweep(t)
	spent := f.ctx.GasMeter().GasConsumed() - before
	require.Less(t, spent, uint64(50_000),
		"one validator must cost a handful of reads: the sweep is O(validators), not O(registry)")
}

// The sweep must run BEFORE the staking EndBlocker, and that ordering is load-bearing rather than incidental: staking computes the block's validator-set update in its own EndBlocker, so a validator jailed after it would keep its consensus power for one more block — the exact window this fix exists to close.
func TestValidatorSweep_RunsBeforeTheStakingEndBlocker(t *testing.T) {
	a := newTestApp(t)
	order := a.ModuleManager.OrderEndBlockers

	posOf := func(name string) int {
		for i, m := range order {
			if m == name {
				return i
			}
		}
		return -1
	}

	identityPos := posOf(identitytypes.ModuleName)
	stakingPos := posOf(stakingtypes.ModuleName)

	require.NotEqual(t, -1, identityPos, "the identity sweep must be in the end-blocker order")
	require.NotEqual(t, -1, stakingPos)
	require.Less(t, identityPos, stakingPos,
		"a validator jailed for a lost identity must be excluded from the validator-set update "+
			"staking emits in this same block, not one block later")
}

// PERMANENCE MUST FOLLOW THE HUMAN, NOT THE BOOKKEEPING.
func TestValidatorSweep_PermanenceFollowsStatusNotBindingPresence(t *testing.T) {
	for _, tc := range []struct {
		name           string
		hasDID         bool
		status         identitytypes.DIDStatus
		wantTombstoned bool
	}{
		{name: "suspended is reversible either way", hasDID: true,
			status: identitytypes.DID_STATUS_SUSPENDED, wantTombstoned: false},
		{name: "revoked is terminal either way", hasDID: true,
			status: identitytypes.DID_STATUS_REVOKED, wantTombstoned: true},
		{name: "no identity is reversible either way", hasDID: false, wantTombstoned: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			permanence := map[bool]bool{}
			for _, bound := range []bool{false, true} {
				label := fmt.Sprintf("perm-%v-%d", bound, tc.status)
				f := newSweepFixture(t, label)
				if tc.hasDID {
					f.registerDID(t, tc.status, bound)
				} else if bound {
					f.a.IdentityKeeper.BindValidatorToDID(f.ctx, "did:phi:absent-"+label, f.valAddr.String())
				}

				f.sweep(t)
				permanence[bound] = f.tombstoned()
				require.Equal(t, tc.wantTombstoned, permanence[bound],
					"bound=%v: permanence must be decided by the DID status", bound)
			}
			require.Equal(t, permanence[false], permanence[true],
				"an operator's fate must not depend on whether a binding record happened to exist")
		})
	}
}
