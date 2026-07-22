// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"testing"
	"time"

	"cosmossdk.io/core/comet"
	"cosmossdk.io/math"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	instkeeper "github.com/Port-PHI/phi-chain/x/institutions/keeper"
	insttypes "github.com/Port-PHI/phi-chain/x/institutions/types"
)

type mockCometValidator struct {
	addr  []byte
	power int64
}

func (m mockCometValidator) Address() []byte { return m.addr }
func (m mockCometValidator) Power() int64    { return m.power }

type mockEquivocation struct {
	val    comet.Validator
	height int64
	at     time.Time
}

func (m mockEquivocation) Type() comet.MisbehaviorType { return comet.DuplicateVote }
func (m mockEquivocation) Validator() comet.Validator  { return m.val }
func (m mockEquivocation) Height() int64               { return m.height }
func (m mockEquivocation) Time() time.Time             { return m.at }
func (m mockEquivocation) TotalVotingPower() int64     { return m.val.Power() }

type mockEvidenceList struct{ items []comet.Evidence }

func (m mockEvidenceList) Len() int                 { return len(m.items) }
func (m mockEvidenceList) Get(i int) comet.Evidence { return m.items[i] }

type mockBlockInfo struct{ ev comet.EvidenceList }

func (m mockBlockInfo) GetEvidence() comet.EvidenceList { return m.ev }
func (m mockBlockInfo) GetValidatorsHash() []byte       { return nil }
func (m mockBlockInfo) GetProposerAddress() []byte      { return nil }
func (m mockBlockInfo) GetLastCommit() comet.CommitInfo { return nil }

// A CometBFT-reported equivocation (double-sign) must be slashed (5%), jailed and tombstoned by the wired x/evidence BeginBlocker.
func TestEvidenceEquivocation_SlashesTombstonesAndConservesSupply(t *testing.T) {
	a := newTestApp(t)
	blockTime := time.Unix(1_700_000_000, 0).UTC()
	ctx := a.NewUncachedContext(false, cmtproto.Header{Height: 10, Time: blockTime})

	sk := a.StakingKeeper
	bondDenom := cointypes.Denom

	sp := stakingtypes.DefaultParams()
	sp.BondDenom = bondDenom
	require.NoError(t, sk.SetParams(ctx, sp))
	slParams := slashingtypes.DefaultParams() // SlashFractionDoubleSign = 5%, as in phiSlashingGenesis
	require.NoError(t, a.SlashingKeeper.SetParams(ctx, slParams))
	require.NoError(t, a.DistrKeeper.FeePool.Set(ctx, distrtypes.InitialFeePool()))

	f1 := sdk.AccAddress([]byte("val1-self-bond-acct_"))
	operator := sdk.AccAddress([]byte("inst-operator-acct__"))
	penalty := sdk.AccAddress([]byte("penalty-destination_"))
	val1 := sdk.ValAddress(f1)

	require.NoError(t, a.InstitutionsKeeper.SetParams(ctx, insttypes.Params{
		Operator:           operator.String(),
		PenaltyDestination: penalty.String(),
		PhiToToman:         insttypes.DefaultPhiToToman,
		RedeemFloorPerTx:   insttypes.DefaultRedeemFloorToman,
	}))

	imsg := instkeeper.NewMsgServerImpl(a.InstitutionsKeeper)
	_, err := imsg.RegisterInstitution(ctx, &insttypes.MsgRegisterInstitution{
		Operator: operator.String(), Id: "bank-a", License: "LIC-1", Admin: operator.String(),
		VaultAccount: "v", VaultApi: "x", Bond: "0", InstitutionType: insttypes.INSTITUTION_TYPE_FINANCIAL,
	})
	require.NoError(t, err)
	compliance := sdk.AccAddress([]byte("compliance-officer__"))
	a.InstitutionsKeeper.SetRole(ctx, "bank-a", compliance, insttypes.INSTITUTION_ROLE_COMPLIANCE)
	a.InstitutionsKeeper.SetRole(ctx, "bank-a", sdk.AccAddress([]byte("second-admin-key____")), insttypes.INSTITUTION_ROLE_ADMIN)
	pinSensitiveThreshold(t, a, ctx, "bank-a")
	_, err = imsg.PublishInstitutionAttestation(ctx, &insttypes.MsgPublishInstitutionAttestation{
		Admin: compliance.String(), Institution: "bank-a", AttestedReserve: "1000000000000",
	})
	require.NoError(t, err)
	_, err = imsg.InstitutionMint(ctx, &insttypes.MsgInstitutionMint{
		Admin: operator.String(), Institution: "bank-a", Recipient: f1.String(),
		AmountToman: "10000000", DepositRef: "fund-f1", // 100 PHI = 100,000,000 uphi
	})
	require.NoError(t, err)

	phi := math.NewIntFromUint64(cointypes.UphiPerPhi)
	consPub := ed25519.GenPrivKeyFromSecret([]byte("val1-cons-secret")).PubKey()
	val, err := stakingtypes.NewValidator(val1.String(), consPub, stakingtypes.Description{Moniker: "val1"})
	require.NoError(t, err)
	val.Status = stakingtypes.Unbonded
	require.NoError(t, sk.SetValidator(ctx, val))
	require.NoError(t, sk.SetValidatorByConsAddr(ctx, val))
	require.NoError(t, sk.SetNewValidatorByPowerIndex(ctx, val))
	require.NoError(t, a.DistrKeeper.Hooks().AfterValidatorCreated(ctx, val1))
	_, err = sk.Delegate(ctx, f1, phi.MulRaw(100), stakingtypes.Unbonded, val, true)
	require.NoError(t, err)
	_, err = sk.EndBlocker(ctx)
	require.NoError(t, err)

	consAddr := sdk.ConsAddress(consPub.Address())
	require.NoError(t, a.SlashingKeeper.AddPubkey(ctx, consPub))
	require.NoError(t, a.SlashingKeeper.SetValidatorSigningInfo(ctx, consAddr,
		slashingtypes.NewValidatorSigningInfo(consAddr, ctx.BlockHeight(), 0, time.Time{}, false, 0)))
	require.False(t, a.SlashingKeeper.IsTombstoned(ctx, consAddr), "precondition: not tombstoned")

	valBefore, err := sk.GetValidator(ctx, val1)
	require.NoError(t, err)
	tokensBefore := valBefore.Tokens
	power := valBefore.ConsensusPower(sk.PowerReduction(ctx))
	require.Positive(t, power)

	supplyBefore := a.BankKeeper.GetSupply(ctx, bondDenom).Amount
	penaltyBefore := a.BankKeeper.GetBalance(ctx, penalty, bondDenom).Amount
	_, broken := instkeeper.SolvencyInvariant(a.InstitutionsKeeper)(ctx)
	require.False(t, broken, "solvency must hold before the equivocation")

	report := mockBlockInfo{ev: mockEvidenceList{items: []comet.Evidence{mockEquivocation{
		val: mockCometValidator{addr: consAddr.Bytes(), power: power}, height: ctx.BlockHeight(), at: blockTime,
	}}}}
	require.NoError(t, a.EvidenceKeeper.BeginBlocker(ctx.WithCometInfo(report)))

	valAfter, err := sk.GetValidator(ctx, val1)
	require.NoError(t, err)
	expectedSlashed := math.LegacyNewDecFromInt(tokensBefore).Mul(slParams.SlashFractionDoubleSign).TruncateInt()
	require.True(t, expectedSlashed.IsPositive(), "the test must actually slash a non-zero amount")
	require.Equal(t, tokensBefore.Sub(expectedSlashed).String(), valAfter.Tokens.String(),
		"a double-sign must slash 5% of the validator's tokens")
	require.True(t, valAfter.IsJailed(), "an equivocating validator must be jailed")
	require.True(t, a.SlashingKeeper.IsTombstoned(ctx, consAddr), "an equivocating validator must be tombstoned")

	supplyAfter := a.BankKeeper.GetSupply(ctx, bondDenom).Amount
	require.Equal(t, supplyBefore.String(), supplyAfter.String(),
		"an equivocation slash must conserve total uphi supply (the burn is compensated)")
	penaltyAfter := a.BankKeeper.GetBalance(ctx, penalty, bondDenom).Amount
	require.Equal(t, expectedSlashed.String(), penaltyAfter.Sub(penaltyBefore).String(),
		"penalty_destination receives exactly the slashed (burned) stake")

	_, broken = instkeeper.SolvencyInvariant(a.InstitutionsKeeper)(ctx)
	require.False(t, broken, "solvency must hold after the equivocation slash")

	require.NoError(t, a.EvidenceKeeper.BeginBlocker(ctx.WithCometInfo(report)))
	require.Equal(t, supplyAfter.String(), a.BankKeeper.GetSupply(ctx, bondDenom).Amount.String(),
		"an already-tombstoned validator is not slashed again")
}
