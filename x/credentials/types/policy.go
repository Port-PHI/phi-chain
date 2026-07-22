// SPDX-License-Identifier: Apache-2.0

package types

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
)

// Per-template disclosure policy: which claim indices of a credential a holder may ever reveal.

const (
	// disclosurePolicyDomain domain-separates the policy hash.
	disclosurePolicyDomain = "phi-disclosure-policy-v1"

	// MaxBbsMessages is the largest claim count a credential may carry (mirrors phi-crypto's MAX_BBS_MESSAGES).
	MaxBbsMessages = 64
)

// ValidateDisclosurePolicy checks a declared policy is well-formed; messageCount == 0 means no policy (strictest).
func ValidateDisclosurePolicy(messageCount uint32, disclosable []uint32) error {
	if messageCount > MaxBbsMessages {
		return fmt.Errorf("message_count %d exceeds the maximum of %d claims", messageCount, MaxBbsMessages)
	}
	if messageCount == 0 && len(disclosable) > 0 {
		return fmt.Errorf("disclosable_indices declared but message_count is 0")
	}
	if len(disclosable) > int(messageCount) {
		return fmt.Errorf("disclosable_indices has %d entries but the credential has only %d claims",
			len(disclosable), messageCount)
	}
	seen := make(map[uint32]struct{}, len(disclosable))
	for _, i := range disclosable {
		// An index past the end of the credential can never be revealed; reject it.
		if i >= messageCount {
			return fmt.Errorf("disclosable index %d is out of range for a %d-claim credential", i, messageCount)
		}
		if _, dup := seen[i]; dup {
			return fmt.Errorf("duplicate disclosable index %d", i)
		}
		seen[i] = struct{}{}
	}
	return nil
}

// DisclosurePolicyHash is the canonical binding of a template's policy: SHA256(domain ‖ 0x00 ‖ id ‖ 0x00 ‖ be32(version) ‖ be32(message_count) ‖ be32(idx)...) with indices sorted-ascending and de-duplicated.
func DisclosurePolicyHash(id string, version, messageCount uint32, disclosable []uint32) []byte {
	idx := canonicalIndices(disclosable)

	h := sha256.New()
	h.Write([]byte(disclosurePolicyDomain))
	h.Write([]byte{0x00})
	h.Write([]byte(id))
	h.Write([]byte{0x00})
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], version)
	h.Write(buf[:])
	binary.BigEndian.PutUint32(buf[:], messageCount)
	h.Write(buf[:])
	for _, i := range idx {
		binary.BigEndian.PutUint32(buf[:], i)
		h.Write(buf[:])
	}
	return h.Sum(nil)
}

func canonicalIndices(in []uint32) []uint32 {
	out := make([]uint32, 0, len(in))
	seen := make(map[uint32]struct{}, len(in))
	for _, i := range in {
		if _, dup := seen[i]; dup {
			continue
		}
		seen[i] = struct{}{}
		out = append(out, i)
	}
	sort.Slice(out, func(a, b int) bool { return out[a] < out[b] })
	return out
}

// IsDisclosable reports whether message index i may be revealed under this template's policy.
func (t CredentialTemplate) IsDisclosable(i uint32) bool {
	for _, d := range t.DisclosableIndices {
		if d == i {
			return true
		}
	}
	return false
}

// PolicyHash recomputes this template's canonical policy hash from its own fields; genesis validation asserts equality.
func (t CredentialTemplate) PolicyHash() []byte {
	return DisclosurePolicyHash(t.Id, t.Version, t.MessageCount, t.DisclosableIndices)
}
