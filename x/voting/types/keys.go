// SPDX-License-Identifier: Apache-2.0

package types

import "github.com/Port-PHI/phi-chain/internal/storeprefix"

// Module constants and KVStore keys for x/voting.
const (
	ModuleName = "voting"
	StoreKey   = ModuleName
	RouterKey  = ModuleName
	// MaxElectionIDLen bounds the election id length (it is length-prefixed in keys).
	MaxElectionIDLen = 255
	// MaxTitleLen bounds the election title (state-bloat guard).
	MaxTitleLen = 256
	// MaxOptionLen bounds each election option string.
	MaxOptionLen = 128
	// MaxNullifierLen bounds the cast-vote nullifier written into a persistent ballot key.
	MaxNullifierLen = 64
	// NullifierPointLen is the exact nullifier width under voting_snark: 48-byte compressed G1 point N = Hₑ^{m₀}.
	NullifierPointLen = 48
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

// BallotKey builds prefix || len(election_id) || election_id || nullifier; length-prefix groups an election's ballots.
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

// AllStorePrefixes is the complete set of KVStore prefixes this module owns (drives the genesis coverage tests).
func AllStorePrefixes() []storeprefix.Prefix {
	return []storeprefix.Prefix{
		{Name: "params", Bytes: ParamsKey},
		{Name: "elections", Bytes: ElectionPrefix},
		{Name: "ballots", Bytes: BallotPrefix},
	}
}
