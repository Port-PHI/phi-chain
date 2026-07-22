// SPDX-License-Identifier: Apache-2.0

package types

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

// DIFFERENTIAL CHECK.
func TestDefaultTable_ReproducesThePreviousHardcodedClassification(t *testing.T) {
	previouslyHardcodedTechnical := []string{
		"/cosmos.staking.v1beta1.MsgUpdateParams",
		"/cosmos.slashing.v1beta1.MsgUpdateParams",
		"/cosmos.distribution.v1beta1.MsgUpdateParams",
		"/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade",
		"/cosmos.upgrade.v1beta1.MsgCancelUpgrade",
		"/cosmos.consensus.v1.MsgUpdateParams",
	}
	table := DefaultParams().RouteTable()

	require.Len(t, table, len(previouslyHardcodedTechnical),
		"the default table must contain exactly the previously hardcoded technical types — no more, no fewer")
	for _, url := range previouslyHardcodedTechnical {
		require.Equal(t, VOTE_ROUTE_TECHNICAL, RouteFor(url, table),
			"%s was technical before the move on-chain and must still be technical", url)
	}

	for _, m := range []sdk.Msg{
		&stakingtypes.MsgUpdateParams{}, &slashingtypes.MsgUpdateParams{}, &distrtypes.MsgUpdateParams{},
		&consensustypes.MsgUpdateParams{},
	} {
		require.Equal(t, VOTE_ROUTE_TECHNICAL, Classify([]sdk.Msg{m}, table),
			"%s must stay TECHNICAL", sdk.MsgTypeURL(m))
	}
	require.Equal(t, VOTE_ROUTE_PUBLIC, Classify([]sdk.Msg{&banktypes.MsgSend{}}, table))
}

// THE ANTI-CAPTURE CORE.
func TestAntiCapture_MappingChangeIsAlwaysPublicEvenAgainstAPoisonedTable(t *testing.T) {
	mappingUpdate := []sdk.Msg{&MsgUpdateParams{}}
	require.Equal(t, MappingUpdateMsgTypeURL, sdk.MsgTypeURL(&MsgUpdateParams{}),
		"the hardcoded anchor must match the real message type URL")

	require.Equal(t, VOTE_ROUTE_PUBLIC, Classify(mappingUpdate, DefaultParams().RouteTable()))

	poisoned := map[string]VoteRoute{MappingUpdateMsgTypeURL: VOTE_ROUTE_TECHNICAL}
	require.Equal(t, VOTE_ROUTE_PUBLIC, RouteFor(MappingUpdateMsgTypeURL, poisoned),
		"a poisoned table entry must be IGNORED — the mapping-update route is fixed in code")
	require.Equal(t, VOTE_ROUTE_PUBLIC, Classify(mappingUpdate, poisoned),
		"an attacker cannot route a mapping change through the validator path by poisoning the table")

	require.Equal(t, VOTE_ROUTE_PUBLIC, Classify(mappingUpdate, map[string]VoteRoute{}))

	bundled := []sdk.Msg{&stakingtypes.MsgUpdateParams{}, &MsgUpdateParams{}}
	require.Equal(t, VOTE_ROUTE_PUBLIC, Classify(bundled, DefaultParams().RouteTable()),
		"a mapping change bundled with a technical message is still decided by the public path")
}

// The poisoned entry cannot even reach state: Validate refuses it at the door, so no operator ever reads the table and believes something false about how a mapping change will be decided.
func TestAntiCapture_ValidateRefusesAMappingUpdateEntryOutright(t *testing.T) {
	for _, route := range []VoteRoute{VOTE_ROUTE_TECHNICAL, VOTE_ROUTE_PUBLIC} {
		p := Params{VoteRoutes: []VoteRouteEntry{{MsgTypeUrl: MappingUpdateMsgTypeURL, Route: route}}}
		require.Error(t, p.Validate(),
			"the mapping-update message is not table-governed; an entry for it (route=%s) must be refused", route)
	}
}

// The fail-safe default: an unmapped or unknown message type is PUBLIC, never silently validator-weighted.
func TestUnknownMsgTypeDefaultsToPublic(t *testing.T) {
	table := DefaultParams().RouteTable()

	require.Equal(t, VOTE_ROUTE_PUBLIC, RouteFor("/some.brand.new.MsgNobodyEnumerated", table))
	require.Equal(t, VOTE_ROUTE_PUBLIC, Classify([]sdk.Msg{&banktypes.MsgSend{}}, table))

	require.Equal(t, VOTE_ROUTE_PUBLIC, Classify([]sdk.Msg{&stakingtypes.MsgUpdateParams{}}, map[string]VoteRoute{}),
		"clearing the table makes everything PUBLIC, never everything technical")

	require.Equal(t, VOTE_ROUTE_PUBLIC, Classify(nil, table))
}

// STRICTEST WINS.
func TestStrictestWins_ABundleWithAnyPublicMessageIsPublic(t *testing.T) {
	table := DefaultParams().RouteTable()

	allTechnical := []sdk.Msg{&consensustypes.MsgUpdateParams{}, &stakingtypes.MsgUpdateParams{}}
	require.Equal(t, VOTE_ROUTE_TECHNICAL, Classify(allTechnical, table),
		"a bundle of purely technical messages is technical")

	smuggled := []sdk.Msg{&consensustypes.MsgUpdateParams{}, &banktypes.MsgSend{}}
	require.Equal(t, VOTE_ROUTE_PUBLIC, Classify(smuggled, table),
		"a public matter bundled with a technical message must NOT be decided by the validator path")

	require.Equal(t, VOTE_ROUTE_PUBLIC,
		Classify([]sdk.Msg{&banktypes.MsgSend{}, &consensustypes.MsgUpdateParams{}}, table))
}

// The table is consensus-critical routing: a malformed entry must be refused by validation, not discovered when a proposal is being classified.
func TestParamsValidate_TableBounds(t *testing.T) {
	require.NoError(t, DefaultParams().Validate())

	cases := map[string]Params{
		"empty msg_type_url": {VoteRoutes: []VoteRouteEntry{{MsgTypeUrl: "", Route: VOTE_ROUTE_TECHNICAL}}},
		"missing leading slash": {VoteRoutes: []VoteRouteEntry{
			{MsgTypeUrl: "cosmos.upgrade.v1beta1.MsgSoftwareUpgrade", Route: VOTE_ROUTE_TECHNICAL}}},
		"unspecified route": {VoteRoutes: []VoteRouteEntry{
			{MsgTypeUrl: "/a.B", Route: VOTE_ROUTE_UNSPECIFIED}}},
		"duplicate entry": {VoteRoutes: []VoteRouteEntry{
			{MsgTypeUrl: "/a.B", Route: VOTE_ROUTE_TECHNICAL},
			{MsgTypeUrl: "/a.B", Route: VOTE_ROUTE_PUBLIC}}},
	}
	for name, p := range cases {
		require.Error(t, p.Validate(), "must reject: %s", name)
	}

	require.NoError(t, Params{}.Validate())
}

// Genesis round-trips the table.
func TestGenesis_RoundTripsTheTable(t *testing.T) {
	gs := DefaultGenesis()
	require.NoError(t, gs.Validate())
	require.Equal(t, DefaultParams().VoteRoutes, gs.Params.VoteRoutes)

	bad := GenesisState{Params: Params{VoteRoutes: []VoteRouteEntry{
		{MsgTypeUrl: MappingUpdateMsgTypeURL, Route: VOTE_ROUTE_TECHNICAL},
	}}}
	require.Error(t, bad.Validate(), "genesis must not be able to seed a poisoned table either")
}
