// SPDX-License-Identifier: Apache-2.0

package types

// Attestation staleness (§4.6): "if an institution's reserve attestation grows older than a governance threshold, that institution's minting is closed." The gate is a PURE, COMPUTED READ.

// EffectiveStaleness returns the staleness threshold that actually applies to an institution: the STRICTER (smaller) of the protocol floor and the institution's own limit.
func EffectiveStaleness(floorSeconds, instLatencySeconds uint64) uint64 {
	if instLatencySeconds == 0 {
		return floorSeconds
	}
	if floorSeconds == 0 || instLatencySeconds < floorSeconds {
		// The institution has opted into a stricter limit than the protocol requires (or than the protocol requires anything at all).
		return instLatencySeconds
	}
	return floorSeconds
}

// IsAttestationStale reports whether an institution's reserve attestation is older than `threshold` at `blockTime`.
func IsAttestationStale(lastAttestedAt, blockTime int64, threshold uint64) bool {
	if threshold == 0 {
		return false
	}
	// Widen to avoid an int64 overflow on an absurd (validated-away, but cheap to guard) threshold.
	deadline := uint64(lastAttestedAt) + threshold
	if lastAttestedAt < 0 {
		return true // a malformed timestamp is stale: fail closed.
	}
	return blockTime >= 0 && uint64(blockTime) > deadline
}

// IsStaleAt is the institution-scoped form: it resolves the effective threshold from the protocol floor and the institution's own limit, then applies the gate.
func (inst Institution) IsStaleAt(blockTime int64, floorSeconds uint64) bool {
	return IsAttestationStale(inst.LastAttestedAt, blockTime,
		EffectiveStaleness(floorSeconds, inst.Params.AutoSuspendRules.MaxVaultAttestationLatencyS))
}
