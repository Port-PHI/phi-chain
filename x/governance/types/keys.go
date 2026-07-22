// SPDX-License-Identifier: Apache-2.0

package types

import (
	"encoding/binary"

	"github.com/Port-PHI/phi-chain/internal/storeprefix"
)

const (
	// ModuleName owns Phi's message-type → vote-path table; distinct from the SDK's x/gov ("gov").
	ModuleName = "governance"
	// StoreKey is NOT ModuleName: the SDK refuses store keys where one is a prefix of another ("gov").
	StoreKey  = "phigovernance"
	RouterKey = ModuleName

	// MappingUpdateMsgTypeURL is the anti-capture anchor: a proposal rewriting the table is fixed PUBLIC in code (RouteFor), never table-classified.
	MappingUpdateMsgTypeURL = "/phi.governance.MsgUpdateParams"
)

// ParamsKey is the key for the single params record.
var ParamsKey = []byte{0x00}

// Prefixes below carry the running one-human-one-vote tally, accumulated per gas-metered vote so the gov EndBlocker reads a finished result rather than walking every vote under an infinite gas meter.
var (
	// TallyCountPrefix: (proposal_id ‖ option) → running ballot count.
	TallyCountPrefix = []byte{0x10}
	// TallyTurnoutPrefix: proposal_id → running count of eligible voters.
	TallyTurnoutPrefix = []byte{0x11}
	// CountedVotePrefix: (proposal_id ‖ voter) → option last counted (exact delta on change), or ineligible marker.
	CountedVotePrefix = []byte{0x12}
	// ProposalEligibilityPrefix: proposal_id → frozen (denominator ‖ cutoff), taken once at voting start.
	ProposalEligibilityPrefix = []byte{0x13}
	// PrunePrefix: proposal_id → deletion-queue marker; deletion affects no result, budgeted across blocks.
	PrunePrefix = []byte{0x14}
)

// GenesisStorePrefixes are the live prefixes exported/imported as raw records so a mid-vote export restores the same tally.
var GenesisStorePrefixes = [][]byte{
	TallyCountPrefix,
	TallyTurnoutPrefix,
	CountedVotePrefix,
	ProposalEligibilityPrefix,
	PrunePrefix,
}

// IsGenesisStoreKey reports whether a raw key lies strictly under one of the exported prefixes.
func IsGenesisStoreKey(key []byte) bool {
	for _, p := range GenesisStorePrefixes {
		if len(key) > len(p) && string(key[:len(p)]) == string(p) {
			return true
		}
	}
	return false
}

// IneligibleVoteMarker is the CountedVote value recorded for a voter rejected at cast time.
const IneligibleVoteMarker byte = 0xFF

// TallyCountKey builds the (proposal_id ‖ option) running-count key.
func TallyCountKey(proposalID uint64, option int32) []byte {
	k := append(append([]byte{}, TallyCountPrefix...), Uint64Key(proposalID)...)
	return append(k, byte(option))
}

// TallyTurnoutKey builds the per-proposal turnout key.
func TallyTurnoutKey(proposalID uint64) []byte {
	return append(append([]byte{}, TallyTurnoutPrefix...), Uint64Key(proposalID)...)
}

// CountedVoteKey builds the (proposal_id ‖ voter) key recording what this voter contributed.
func CountedVoteKey(proposalID uint64, voter []byte) []byte {
	k := append(append([]byte{}, CountedVotePrefix...), Uint64Key(proposalID)...)
	return append(k, voter...)
}

// CountedVotePrefixFor returns the iteration prefix for one proposal's recorded contributions.
func CountedVotePrefixFor(proposalID uint64) []byte {
	return append(append([]byte{}, CountedVotePrefix...), Uint64Key(proposalID)...)
}

// ProposalEligibilityKey builds the per-proposal frozen-eligibility key.
func ProposalEligibilityKey(proposalID uint64) []byte {
	return append(append([]byte{}, ProposalEligibilityPrefix...), Uint64Key(proposalID)...)
}

// PruneKey builds the per-proposal pruning-queue key.
func PruneKey(proposalID uint64) []byte {
	return append(append([]byte{}, PrunePrefix...), Uint64Key(proposalID)...)
}

// Uint64Key encodes a proposal id big-endian, so keys sort in id order.
func Uint64Key(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

// AllStorePrefixes is the COMPLETE set of KVStore prefixes this module owns.
func AllStorePrefixes() []storeprefix.Prefix {
	return []storeprefix.Prefix{
		{Name: "params", Bytes: ParamsKey},
		{Name: "tally_counts", Bytes: TallyCountPrefix},
		{Name: "tally_turnout", Bytes: TallyTurnoutPrefix},
		{Name: "counted_votes", Bytes: CountedVotePrefix},
		{Name: "proposal_eligibility", Bytes: ProposalEligibilityPrefix},
		{Name: "prune_queue", Bytes: PrunePrefix},
	}
}
