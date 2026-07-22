// SPDX-License-Identifier: Apache-2.0

package types

import (
	"encoding/binary"
	"fmt"
)

// Reading the revealed set out of a BBS+ selective-disclosure proof.

// MaxBbsMessages mirrors phi-crypto's MAX_BBS_MESSAGES: no credential may carry more claims.
const MaxBbsMessages = 64

// DisclosedProof is what the chain can learn about a proof without verifying it.
type DisclosedProof struct {
	// MessageCount is the claim count of the credential the proof was derived from.
	MessageCount uint32
	// RevealedIndices are the message indices the proof discloses, ascending and de-duplicated.
	RevealedIndices []uint32
}

// IsPredicate reports whether the proof reveals no raw claim (L1).
func (d DisclosedProof) IsPredicate() bool { return len(d.RevealedIndices) == 0 }

// Level classifies the proof by what it actually discloses: L1 if it reveals nothing, L2 if it reveals any claim.
func (d DisclosedProof) Level() DisclosureLevel {
	if d.IsPredicate() {
		return DISCLOSURE_LEVEL_PREDICATE
	}
	return DISCLOSURE_LEVEL_PARTIAL
}

// ParseDisclosedProof reads the SelectiveProof envelope and reports what the proof reveals.
func ParseDisclosedProof(b []byte) (DisclosedProof, error) {
	r := reader{b: b}

	messageCount, err := r.u32()
	if err != nil {
		return DisclosedProof{}, err
	}
	// Mirrors phi-crypto's own bounds: a zero count is degenerate and an oversized one is not signable, so neither can describe a real credential.
	if messageCount == 0 || messageCount > MaxBbsMessages {
		return DisclosedProof{}, fmt.Errorf("message_count %d out of range (1..%d)", messageCount, MaxBbsMessages)
	}
	if _, err := r.bytes(); err != nil { // the proof body — skipped, never interpreted here
		return DisclosedProof{}, err
	}
	n, err := r.u32()
	if err != nil {
		return DisclosedProof{}, err
	}
	if n > messageCount {
		return DisclosedProof{}, fmt.Errorf("proof reveals %d claims but the credential has %d", n, messageCount)
	}

	seen := make(map[uint32]struct{}, n)
	indices := make([]uint32, 0, n)
	for i := uint32(0); i < n; i++ {
		idx, err := r.u32()
		if err != nil {
			return DisclosedProof{}, err
		}
		if _, err := r.bytes(); err != nil { // the revealed claim value — the chain does not look at it
			return DisclosedProof{}, err
		}
		if idx >= messageCount {
			return DisclosedProof{}, fmt.Errorf("revealed index %d is out of range for a %d-claim credential", idx, messageCount)
		}
		if _, dup := seen[idx]; dup {
			// The same index revealed twice is a malformed proof, not a clever one.
			return DisclosedProof{}, fmt.Errorf("index %d revealed more than once", idx)
		}
		seen[idx] = struct{}{}
		indices = append(indices, idx)
	}
	// Trailing bytes mean the envelope is not what it claims to be.
	if r.pos != len(b) {
		return DisclosedProof{}, fmt.Errorf("%d trailing bytes after the proof envelope", len(b)-r.pos)
	}

	sortAscending(indices)
	return DisclosedProof{MessageCount: messageCount, RevealedIndices: indices}, nil
}

func sortAscending(a []uint32) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

type reader struct {
	b   []byte
	pos int
}

func (r *reader) u32() (uint32, error) {
	if r.pos+4 > len(r.b) {
		return 0, fmt.Errorf("truncated proof envelope: want 4 bytes at offset %d, have %d", r.pos, len(r.b)-r.pos)
	}
	v := binary.BigEndian.Uint32(r.b[r.pos : r.pos+4])
	r.pos += 4
	return v, nil
}

func (r *reader) bytes() ([]byte, error) {
	n, err := r.u32()
	if err != nil {
		return nil, err
	}
	if int64(n) > int64(len(r.b)-r.pos) {
		return nil, fmt.Errorf("truncated proof envelope: length prefix %d exceeds the %d bytes remaining", n, len(r.b)-r.pos)
	}
	out := r.b[r.pos : r.pos+int(n)]
	r.pos += int(n)
	return out, nil
}
