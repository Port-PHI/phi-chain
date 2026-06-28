// SPDX-License-Identifier: Apache-2.0

package types

// Event types and attribute keys for x/voting.
const (
	EventTypeCreateElection = "create_election"
	EventTypeCastVote       = "cast_vote"
	EventTypeCloseElection  = "close_election"
	EventTypeCancelElection = "cancel_election"

	AttributeKeyElectionID  = "election_id"
	AttributeKeyCreator     = "creator"
	AttributeKeyOptionIndex = "option_index"
	AttributeKeyNullifier   = "nullifier"
	AttributeKeyTotalVotes  = "total_votes"
)
