// SPDX-License-Identifier: Apache-2.0

package types

// Module constants and KVStore keys for x/voting.
const (
	// ModuleName is the module name.
	ModuleName = "voting"
	// StoreKey is the primary KVStore key.
	StoreKey = ModuleName
	// RouterKey is the message route.
	RouterKey = ModuleName
	// MaxElectionIDLen bounds the election id length (it is length-prefixed in keys).
	MaxElectionIDLen = 255
	// MaxTitleLen bounds the election title — a state-bloat guard so a one-time fee cannot store an
	// unbounded blob.
	MaxTitleLen = 256
	// MaxOptionLen bounds each election option string.
	MaxOptionLen = 128
	// MaxNullifierLen bounds the cast-vote nullifier written into a persistent ballot key.
	MaxNullifierLen = 64
)

// KVStore key prefixes.
var (
	// ParamsKey is the single-record params key.
	ParamsKey = []byte{0x00}
	// ElectionPrefix prefixes id -> Election.
	ElectionPrefix = []byte{0x10}
	// BallotPrefix prefixes (election_id, nullifier) -> Ballot.
	BallotPrefix = []byte{0x20}
)

// ElectionKey builds the storage key for an election.
func ElectionKey(id string) []byte {
	return append(append([]byte{}, ElectionPrefix...), []byte(id)...)
}

// BallotKey builds the composite storage key for a ballot:
// prefix || len(election_id) || election_id || nullifier. The election id is
// length-prefixed (one byte) so a single election's ballots group for iteration.
func BallotKey(electionID string, nullifier []byte) []byte {
	return append(ballotElectionPrefix(electionID), nullifier...)
}

// BallotElectionPrefix builds the iteration prefix for one election's ballots.
func BallotElectionPrefix(electionID string) []byte {
	return ballotElectionPrefix(electionID)
}

func ballotElectionPrefix(electionID string) []byte {
	id := []byte(electionID)
	key := append(append([]byte{}, BallotPrefix...), byte(len(id)))
	return append(key, id...)
}
