// SPDX-License-Identifier: Apache-2.0

package types

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultTechnicalMsgTypeURLs is the genesis vote-route table: the enumerated TECHNICAL exceptions.
var DefaultTechnicalMsgTypeURLs = []string{
	"/cosmos.consensus.v1.MsgUpdateParams",
	"/cosmos.distribution.v1beta1.MsgUpdateParams",
	"/cosmos.slashing.v1beta1.MsgUpdateParams",
	"/cosmos.staking.v1beta1.MsgUpdateParams",
	"/cosmos.upgrade.v1beta1.MsgCancelUpgrade",
	"/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade",
}

// DefaultParams returns the default vote-route table.
func DefaultParams() Params {
	entries := make([]VoteRouteEntry, 0, len(DefaultTechnicalMsgTypeURLs))
	for _, url := range DefaultTechnicalMsgTypeURLs {
		entries = append(entries, VoteRouteEntry{MsgTypeUrl: url, Route: VOTE_ROUTE_TECHNICAL})
	}
	return Params{VoteRoutes: entries}
}

// Validate checks the vote-route table.
func (p Params) Validate() error {
	seen := make(map[string]bool, len(p.VoteRoutes))
	for _, e := range p.VoteRoutes {
		if e.MsgTypeUrl == "" {
			return fmt.Errorf("vote_route entry with empty msg_type_url")
		}
		if !strings.HasPrefix(e.MsgTypeUrl, "/") {
			return fmt.Errorf("vote_route msg_type_url %q must start with '/'", e.MsgTypeUrl)
		}
		// Routing must not depend on table order: a duplicate row would make one of the two entries dead, and which one survived would be an artefact of iteration order.
		if seen[e.MsgTypeUrl] {
			return fmt.Errorf("duplicate vote_route entry for %s", e.MsgTypeUrl)
		}
		seen[e.MsgTypeUrl] = true
		if e.Route != VOTE_ROUTE_PUBLIC && e.Route != VOTE_ROUTE_TECHNICAL {
			return fmt.Errorf("vote_route for %s must be PUBLIC or TECHNICAL, got %s", e.MsgTypeUrl, e.Route)
		}
		// ANTI-CAPTURE, enforced at the door as well as at the classifier.
		if e.MsgTypeUrl == MappingUpdateMsgTypeURL {
			return fmt.Errorf("%s is not table-governed: a mapping change is always decided on the PUBLIC path", MappingUpdateMsgTypeURL)
		}
	}
	return nil
}

// RouteTable returns the params as a lookup map.
func (p Params) RouteTable() map[string]VoteRoute {
	m := make(map[string]VoteRoute, len(p.VoteRoutes))
	for _, e := range p.VoteRoutes {
		m[e.MsgTypeUrl] = e.Route
	}
	return m
}

// RouteFor returns the vote path for ONE message type.
func RouteFor(msgTypeURL string, table map[string]VoteRoute) VoteRoute {
	if msgTypeURL == MappingUpdateMsgTypeURL {
		return VOTE_ROUTE_PUBLIC
	}
	if table[msgTypeURL] == VOTE_ROUTE_TECHNICAL {
		return VOTE_ROUTE_TECHNICAL
	}
	return VOTE_ROUTE_PUBLIC
}

// Classify determines a PROPOSAL's vote path from its messages.
func Classify(msgs []sdk.Msg, table map[string]VoteRoute) VoteRoute {
	for _, m := range msgs {
		if RouteFor(sdk.MsgTypeURL(m), table) == VOTE_ROUTE_PUBLIC {
			return VOTE_ROUTE_PUBLIC
		}
	}
	if len(msgs) == 0 {
		return VOTE_ROUTE_PUBLIC
	}
	return VOTE_ROUTE_TECHNICAL
}
