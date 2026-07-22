// SPDX-License-Identifier: Apache-2.0

package governance

import (
	"testing"
	"time"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/governance/types"
)

type publicRoutes struct{}

func (publicRoutes) VoteRouteTable(sdk.Context) map[string]types.VoteRoute {
	return map[string]types.VoteRoute{}
}

type defaultRoutes struct{}

func (defaultRoutes) VoteRouteTable(sdk.Context) map[string]types.VoteRoute {
	return types.DefaultParams().RouteTable()
}

type noValidators struct{}

func (noValidators) IsActiveValidatorOperator(sdk.Context, []byte) bool { return false }

type validatorSet struct{ bonded map[string]bool }

func (v validatorSet) IsActiveValidatorOperator(_ sdk.Context, addr []byte) bool {
	return v.bonded[string(addr)]
}

func technicalProposal(t *testing.T, id uint64, start int64) v1.Proposal {
	t.Helper()
	msg, err := codectypes.NewAnyWithValue(&stakingtypes.MsgUpdateParams{
		Authority: sdk.AccAddress([]byte("gov_authority_______")).String(),
		Params:    stakingtypes.DefaultParams(),
	})
	require.NoError(t, err)

	p := proposalAt(id, start)
	p.Messages = []*codectypes.Any{msg}
	return p
}

type noIdentity struct{ denominator uint64 }

func (noIdentity) IsEligibleControllerAt(sdk.Context, string, time.Time, time.Duration) bool {
	return false
}

func (noIdentity) IsEligibleControllerSince(sdk.Context, string, time.Time, time.Duration, time.Time) bool {
	return false
}

func (n noIdentity) CountEligibleControllersAt(sdk.Context, time.Time, time.Duration) uint64 {
	return n.denominator
}
func (n noIdentity) EligibleControllerTotal(sdk.Context) uint64 { return n.denominator }
func (noIdentity) MinIdentityAge(sdk.Context) time.Duration     { return 0 }

// TestVoteRoute_ValidatorWithoutAnAgedDIDCanVoteOnATechnicalProposal is the regression this fix exists for: the aged-DID rule must not reach the technical route at all.
func TestVoteRoute_ValidatorWithoutAnAgedDIDCanVoteOnATechnicalProposal(t *testing.T) {
	ctx, k := govSetup(t)
	now := int64(6_000_000)
	ctx = ctx.WithBlockTime(time.Unix(now, 0))

	validator := voterAddr(1)
	h := NewVoteHooks(k, nil, noIdentity{denominator: 100}, defaultRoutes{},
		validatorSet{bonded: map[string]bool{string(validator.Bytes()): true}})

	proposal := technicalProposal(t, 21, now)
	require.Equal(t, RouteTechnical, h.routeOf(ctx, proposal), "the fixture must be a technical proposal")

	err := h.recordBallot(ctx, proposal, validator.String(), validator.Bytes(), single(v1.OptionYes))
	require.NoError(t, err,
		"a bonded validator votes on a technical proposal regardless of whether it holds an aged DID")

	require.Zero(t, k.GetRunningTally(ctx, proposal.Id).Turnout,
		"the technical route must not feed the per-head running tally")
}

// The other half of the branch: a non-validator gets nothing from the technical route either.
func TestVoteRoute_NonValidatorIsRefusedOnATechnicalProposal(t *testing.T) {
	ctx, k := govSetup(t)
	now := int64(6_000_000)
	ctx = ctx.WithBlockTime(time.Unix(now, 0))

	validator := voterAddr(1)
	outsider := voterAddr(2)
	h := NewVoteHooks(k, nil, newModelRegistryWithAll(&now, outsider.String()), defaultRoutes{},
		validatorSet{bonded: map[string]bool{string(validator.Bytes()): true}})

	proposal := technicalProposal(t, 22, now)
	err := h.recordBallot(ctx, proposal, outsider.String(), outsider.Bytes(), single(v1.OptionYes))
	require.ErrorIs(t, err, types.ErrNotEligibleToVote,
		"holding an aged DID does not admit a ballot on the validator route")
}

// The public route is unchanged: aged-DID eligibility still decides, and a validator gets no shortcut.
func TestVoteRoute_PublicProposalsStillUseAgedDIDEligibility(t *testing.T) {
	ctx, k := govSetup(t)
	now := int64(6_000_000)
	ctx = ctx.WithBlockTime(time.Unix(now, 0))

	eligible := voterAddr(3)
	validatorOnly := voterAddr(4)
	reg := newModelRegistryWithAll(&now, eligible.String())

	h := NewVoteHooks(k, nil, reg, defaultRoutes{},
		validatorSet{bonded: map[string]bool{string(validatorOnly.Bytes()): true}})

	proposal := proposalAt(23, now) // no messages -> PUBLIC
	require.Equal(t, RoutePublic, h.routeOf(ctx, proposal))

	require.NoError(t, h.recordBallot(ctx, proposal, eligible.String(), eligible.Bytes(),
		single(v1.OptionYes)))
	require.Equal(t, uint64(1), k.GetRunningTally(ctx, proposal.Id).Turnout)

	err := h.recordBallot(ctx, proposal, validatorOnly.String(), validatorOnly.Bytes(),
		single(v1.OptionYes))
	require.ErrorIs(t, err, types.ErrNotEligibleToVote,
		"being a validator does not admit a ballot on the public route")
	require.Equal(t, uint64(1), k.GetRunningTally(ctx, proposal.Id).Turnout)
}

// A proposal bundling a technical message with a public one is PUBLIC (strictest wins), so the aged-DID rule applies to it — the route branch must read the whole proposal, not the first message.
func TestVoteRoute_BundledProposalsAreJudgedByThePublicRule(t *testing.T) {
	ctx, k := govSetup(t)
	now := int64(6_000_000)
	ctx = ctx.WithBlockTime(time.Unix(now, 0))

	validator := voterAddr(5)
	h := NewVoteHooks(k, nil, noIdentity{denominator: 10}, defaultRoutes{},
		validatorSet{bonded: map[string]bool{string(validator.Bytes()): true}})

	public, err := codectypes.NewAnyWithValue(&types.MsgUpdateParams{
		Authority: sdk.AccAddress([]byte("gov_authority_______")).String(),
	})
	require.NoError(t, err)

	proposal := technicalProposal(t, 24, now)
	proposal.Messages = append(proposal.Messages, public)
	require.Equal(t, RoutePublic, h.routeOf(ctx, proposal))

	err = h.recordBallot(ctx, proposal, validator.String(), validator.Bytes(), single(v1.OptionYes))
	require.ErrorIs(t, err, types.ErrNotEligibleToVote,
		"a bundle carrying a public message is decided on the public route")
}

func newModelRegistryWithAll(now *int64, controllers ...string) *modelRegistry {
	reg := newModelRegistry(now)
	for _, c := range controllers {
		reg.register(c, *now-100_000)
	}
	return reg
}
