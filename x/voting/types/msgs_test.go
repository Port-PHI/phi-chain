// SPDX-License-Identifier: Apache-2.0

package types

import (
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// Title, options and nullifier are length-bounded.
func TestMsgValidateBasic_LengthBounds(t *testing.T) {
	creator := sdk.AccAddress([]byte("creator_____________")).String()
	ok := &MsgCreateElection{Creator: creator, Id: "e1", Title: "Q?", Options: []string{"yes", "no"}, RequiredTemplateId: "tmpl", VotingStart: 0, VotingEnd: 100}
	require.NoError(t, ok.ValidateBasic())

	overTitle := *ok
	overTitle.Title = strings.Repeat("x", MaxTitleLen+1)
	require.Error(t, overTitle.ValidateBasic(), "over-length title must be rejected")

	overOpt := *ok
	overOpt.Options = []string{"yes", strings.Repeat("x", MaxOptionLen+1)}
	require.Error(t, overOpt.ValidateBasic(), "over-length option must be rejected")

	voter := sdk.AccAddress([]byte("voter_______________")).String()
	vote := &MsgCastVote{Voter: voter, ElectionId: "e1", Nullifier: []byte("n"), EligibilityProof: []byte("p")}
	require.NoError(t, vote.ValidateBasic())

	overNull := *vote
	overNull.Nullifier = make([]byte, MaxNullifierLen+1)
	require.Error(t, overNull.ValidateBasic(), "over-length nullifier must be rejected")
}
