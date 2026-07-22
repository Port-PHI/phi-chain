// SPDX-License-Identifier: Apache-2.0

package types_test

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/disclosure/types"
)

func envelope(messageCount uint32, body []byte, revealed ...uint32) []byte {
	var out []byte
	be := func(v uint32) {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], v)
		out = append(out, b[:]...)
	}
	be(messageCount)
	be(uint32(len(body)))
	out = append(out, body...)
	be(uint32(len(revealed)))
	for _, i := range revealed {
		be(i)
		claim := []byte("v")
		be(uint32(len(claim)))
		out = append(out, claim...)
	}
	return out
}

func TestParseDisclosedProof_ReadsTheRevealedSet(t *testing.T) {
	d, err := types.ParseDisclosedProof(envelope(4, []byte("proof-body"), 2, 0))
	require.NoError(t, err)
	require.Equal(t, uint32(4), d.MessageCount)
	require.Equal(t, []uint32{0, 2}, d.RevealedIndices, "reported ascending regardless of the order in the proof")
	require.False(t, d.IsPredicate())
	require.Equal(t, types.DISCLOSURE_LEVEL_PARTIAL, d.Level())
}

// An empty revealed list is the L1 predicate case: the proof discloses no raw claim.
func TestParseDisclosedProof_PredicateRevealsNothing(t *testing.T) {
	d, err := types.ParseDisclosedProof(envelope(4, []byte("proof-body")))
	require.NoError(t, err)
	require.True(t, d.IsPredicate())
	require.Empty(t, d.RevealedIndices)
	require.Equal(t, types.DISCLOSURE_LEVEL_PREDICATE, d.Level())
}

// Everything the parser cannot account for is refused.
func TestParseDisclosedProof_FailsClosed(t *testing.T) {
	good := envelope(4, []byte("proof-body"), 1)

	cases := map[string][]byte{
		"empty":                       {},
		"truncated header":            good[:3],
		"truncated mid-body":          good[:9],
		"truncated revealed entry":    good[:len(good)-2],
		"trailing bytes":              append(append([]byte{}, good...), 0xff),
		"message_count zero":          envelope(0, []byte("b")),
		"message_count above max":     envelope(types.MaxBbsMessages+1, []byte("b")),
		"revealed index out of range": envelope(4, []byte("b"), 4),
		"duplicate revealed index":    envelope(4, []byte("b"), 1, 1),
		"reveals more than it has":    envelope(1, []byte("b"), 0, 0, 0),
	}
	for name, b := range cases {
		_, err := types.ParseDisclosedProof(b)
		require.Error(t, err, "%s must be rejected", name)
	}

	hostile := []byte{0, 0, 0, 4, 0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0}
	_, err := types.ParseDisclosedProof(hostile)
	require.Error(t, err)
}

// The reveal list is what phi-crypto feeds into its pairing check, so it is bound by the cryptography — but the ENVELOPE around it is what this parser reads.
func TestParseDisclosedProof_ByteLayoutIsPinned(t *testing.T) {
	b := []byte{
		0, 0, 0, 3, // message_count = 3
		0, 0, 0, 2, 0xAA, 0xBB, // proof body (2 bytes)
		0, 0, 0, 1, // one revealed claim
		0, 0, 0, 2, // index 2
		0, 0, 0, 3, 'y', 'e', 's', // claim "yes"
	}
	d, err := types.ParseDisclosedProof(b)
	require.NoError(t, err)
	require.Equal(t, uint32(3), d.MessageCount)
	require.Equal(t, []uint32{2}, d.RevealedIndices)
}
