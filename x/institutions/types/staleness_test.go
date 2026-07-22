// SPDX-License-Identifier: Apache-2.0

package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const hour = int64(3600)

// The gate is a pure function of (last_attested_at, block_time, threshold).
func TestIsAttestationStale_Boundary(t *testing.T) {
	const attestedAt = int64(1_700_000_000)
	threshold := uint64(24 * hour)

	require.False(t, IsAttestationStale(attestedAt, attestedAt, threshold), "just attested")
	require.False(t, IsAttestationStale(attestedAt, attestedAt+int64(threshold)-1, threshold),
		"one second before the deadline is fresh")
	require.False(t, IsAttestationStale(attestedAt, attestedAt+int64(threshold), threshold),
		"EXACTLY at the deadline is still fresh (inclusive boundary)")
	require.True(t, IsAttestationStale(attestedAt, attestedAt+int64(threshold)+1, threshold),
		"one second past the deadline is STALE")
	require.True(t, IsAttestationStale(attestedAt, attestedAt+30*24*hour, threshold),
		"a month later is thoroughly stale")
}

// threshold = 0 disables the gate entirely, exactly like protocol_fee_bps = 0.
func TestIsAttestationStale_ZeroDisables(t *testing.T) {
	require.False(t, IsAttestationStale(0, 1_700_000_000, 0),
		"threshold 0 disables the gate — even a never-attested institution is not stale")
	require.False(t, IsAttestationStale(1, 1_000_000_000_000, 0))
}

// last_attested_at == 0 means NEVER ATTESTED: stale as soon as the gate is enabled.
func TestIsAttestationStale_NeverAttestedIsStale(t *testing.T) {
	require.True(t, IsAttestationStale(0, 1_700_000_000, uint64(24*hour)),
		"never attested = stale while the gate is on")

	require.True(t, IsAttestationStale(-1, 1_700_000_000, uint64(24*hour)))
}

// STRICTER-ONLY (§4.9).
func TestEffectiveStaleness_StricterWins(t *testing.T) {
	const floor = uint64(24 * 3600)

	require.Equal(t, floor, EffectiveStaleness(floor, 0), "unset institution limit -> the protocol floor")
	require.Equal(t, uint64(3600), EffectiveStaleness(floor, 3600), "a STRICTER institution limit wins")
	require.Equal(t, floor, EffectiveStaleness(floor, floor), "equal -> the same value")

	require.Equal(t, floor, EffectiveStaleness(floor, 90*24*3600),
		"a looser institution limit must NOT weaken the protocol floor")

	require.Equal(t, uint64(3600), EffectiveStaleness(0, 3600))
	require.Equal(t, uint64(0), EffectiveStaleness(0, 0), "both unset -> no gate at all")
}

// The institution-scoped form resolves the effective threshold and applies the gate together.
func TestInstitution_IsStaleAt(t *testing.T) {
	const attestedAt = int64(1_700_000_000)
	inst := Institution{Id: "bank-a", LastAttestedAt: attestedAt}

	require.False(t, inst.IsStaleAt(attestedAt+12*hour, uint64(24*hour)))
	require.True(t, inst.IsStaleAt(attestedAt+25*hour, uint64(24*hour)))

	inst.Params.AutoSuspendRules.MaxVaultAttestationLatencyS = uint64(hour)
	require.True(t, inst.IsStaleAt(attestedAt+2*hour, uint64(24*hour)),
		"a stricter institution limit makes it stale sooner than the floor would")
	require.False(t, inst.IsStaleAt(attestedAt+hour, uint64(24*hour)), "still inside its own limit")
}

// The governed floor is bounded and its default is the documented 24h.
func TestParamsValidate_StalenessFloor(t *testing.T) {
	p := DefaultParams()
	require.NoError(t, p.Validate())
	require.Equal(t, uint64(24*3600), p.MaxAttestationStalenessSeconds, "the default floor is 24 hours")

	p.MaxAttestationStalenessSeconds = 0
	require.NoError(t, p.Validate(), "0 is valid: it disables the gate")

	p.MaxAttestationStalenessSeconds = MaxAttestationStalenessCeiling
	require.NoError(t, p.Validate())

	p.MaxAttestationStalenessSeconds = MaxAttestationStalenessCeiling + 1
	require.Error(t, p.Validate(),
		"governance must not be able to set a threshold so long that a stale attestation is never stale")
}
