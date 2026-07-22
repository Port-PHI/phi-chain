// SPDX-License-Identifier: Apache-2.0

package ante_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"

	phiante "github.com/Port-PHI/phi-chain/app/ante"
)

type msgsTx struct {
	sdk.Tx
	msgs []sdk.Msg
}

func (t msgsTx) GetMsgs() []sdk.Msg { return t.msgs }

type stubVaults struct{ total math.Int }

func (s stubVaults) SumVaultBalance(sdk.Context) math.Int { return s.total }

func runGovGuard(t *testing.T, vaults math.Int, msgs ...sdk.Msg) error {
	t.Helper()
	d := phiante.NewRejectUnsafeGovParamsDecorator(stubVaults{total: vaults})
	noop := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }
	_, err := d.AnteHandle(sdk.Context{}, msgsTx{msgs: msgs}, false, noop)
	return err
}

func safeGovParams() govv1.Params {
	p := govv1.DefaultParams()
	p.BurnVoteVeto = false
	p.BurnVoteQuorum = false
	p.BurnProposalDepositPrevote = false
	p.ProposalCancelDest = "phi1cancellationdestination"
	return p
}

// A MsgUpdateParams arming a uphi deposit burn is rejected while vaults are non-zero, allowed when empty; uphi is vault-backed.
func TestRejectUnsafeGovParams_DepositBurnGuard(t *testing.T) {
	nonZero := math.NewInt(1)
	zero := math.ZeroInt()
	const authority = "phi1govauthority"

	cases := map[string]func(*govv1.Params){
		"BurnVoteVeto":               func(p *govv1.Params) { p.BurnVoteVeto = true },
		"BurnVoteQuorum":             func(p *govv1.Params) { p.BurnVoteQuorum = true },
		"BurnProposalDepositPrevote": func(p *govv1.Params) { p.BurnProposalDepositPrevote = true },
		"ClearProposalCancelDest":    func(p *govv1.Params) { p.ProposalCancelDest = "" },
	}
	for name, mutate := range cases {
		p := safeGovParams()
		mutate(&p)
		msg := &govv1.MsgUpdateParams{Authority: authority, Params: p}

		require.ErrorIs(t, runGovGuard(t, nonZero, msg), sdkerrors.ErrInvalidRequest,
			"%s must be rejected while vaults are non-zero", name)
		require.NoError(t, runGovGuard(t, zero, msg),
			"%s may be enabled while all vaults are empty (pre-launch)", name)
	}

	safe := &govv1.MsgUpdateParams{Authority: authority, Params: safeGovParams()}
	require.NoError(t, runGovGuard(t, nonZero, safe), "a non-burn-enabling param change must pass")
}

// The unsafe MsgUpdateParams wrapped in a MsgSubmitProposal is unwrapped and rejected.
func TestRejectUnsafeGovParams_WrappedInProposal(t *testing.T) {
	nonZero := math.NewInt(1)
	const authority = "phi1govauthority"

	unsafe := safeGovParams()
	unsafe.BurnVoteVeto = true
	inner := &govv1.MsgUpdateParams{Authority: authority, Params: unsafe}
	prop, err := govv1.NewMsgSubmitProposal([]sdk.Msg{inner}, nil, authority, "meta", "title", "summary", false)
	require.NoError(t, err)

	require.ErrorIs(t, runGovGuard(t, nonZero, prop), sdkerrors.ErrInvalidRequest,
		"an unsafe param change wrapped in a governance proposal must be rejected while vaults are non-zero")
	require.NoError(t, runGovGuard(t, math.ZeroInt(), prop),
		"the wrapped change is allowed when all vaults are empty")

	safeInner := &govv1.MsgUpdateParams{Authority: authority, Params: safeGovParams()}
	safeProp, err := govv1.NewMsgSubmitProposal([]sdk.Msg{safeInner}, nil, authority, "meta", "title", "summary", false)
	require.NoError(t, err)
	require.NoError(t, runGovGuard(t, nonZero, safeProp), "a proposal with a safe param change must pass")
}
