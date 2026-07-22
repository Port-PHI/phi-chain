// SPDX-License-Identifier: Apache-2.0

package types_test

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

const (
	otherChain = "phi-testnet-1"
	thisChain  = "phi-mainnet-1"
)

// The framing is exactly u32be(len) ‖ value, for the domain and every field.
func TestCanonicalMessage_Framing(t *testing.T) {
	got := types.CanonicalMessage("dom", []byte("ab"), []byte{})

	want := []byte{
		0, 0, 0, 3, 'd', 'o', 'm',
		0, 0, 0, 2, 'a', 'b',
		0, 0, 0, 0,
	}
	require.Equal(t, want, got)

	require.Equal(t, []byte{0, 0, 0, 3, 'd', 'o', 'm'}, types.CanonicalMessage("dom"))

	long := make([]byte, 300)
	framed := types.CanonicalMessage("d", long)
	require.Equal(t, uint32(300), binary.BigEndian.Uint32(framed[4+1:4+1+4]))
}

// THE INJECTIVITY PROPERTY, and the reason the encoding changed.
func TestCanonicalMessage_FieldBoundaryCannotShift(t *testing.T) {
	a := types.CanonicalMessage("dom", []byte("a"), []byte("b"))
	b := types.CanonicalMessage("dom", []byte("a\x00b"), []byte(""))
	require.NotEqual(t, a, b, "a field containing 0x00 must not collide with a different field split")

	require.NotEqual(t,
		types.CanonicalMessage("dom", []byte("ab"), []byte("c")),
		types.CanonicalMessage("dom", []byte("a"), []byte("bc")))

	require.NotEqual(t,
		types.CanonicalMessage("dom", []byte("a"), []byte{}),
		types.CanonicalMessage("dom", []byte("a")))
}

// A domain that is a PREFIX of another must not let one domain's message be read as the other's.
func TestCanonicalMessage_DomainCannotBeAPrefixOfAnother(t *testing.T) {
	require.NotEqual(t,
		types.CanonicalMessage("phi-recovery", []byte("-v3"), []byte("x")),
		types.CanonicalMessage("phi-recovery-v3", []byte("x")))
}

// Each domain separates its own message family: the same fields under different domains never collide.
func TestCanonicalMessage_DomainsSeparate(t *testing.T) {
	fields := [][]byte{[]byte(thisChain), []byte("did:phi:abc"), []byte("key")}
	require.NotEqual(t,
		types.CanonicalMessage(types.SocialRecoveryPoPDomain, fields...),
		types.CanonicalMessage(types.ReauthRecoveryDomain, fields...))
}

// CROSS-CHAIN REPLAY — social-recovery proof-of-possession.
func TestSocialRecoveryPoPMessage_BindsChainID(t *testing.T) {
	args := func(chainID string) []byte {
		return types.SocialRecoveryPoPMessage(chainID, "did:phi:abc", []byte("new-pub-key"), "phi1ctrl", []byte("nonce"))
	}
	require.NotEqual(t, args(thisChain), args(otherChain), "the same recovery on two chains must not share bytes")
	require.Equal(t, args(thisChain), args(thisChain), "and it must be deterministic on one chain")
}

// The same for the REAUTH attestation.
func TestReauthAttestationMessage_BindsChainID(t *testing.T) {
	args := func(chainID string) []byte {
		return types.ReauthAttestationMessage(chainID, "did:phi:abc", []byte("new-pub-key"), "phi1ctrl", []byte("uniq"), []byte("nonce"))
	}
	require.NotEqual(t, args(thisChain), args(otherChain))
	require.Equal(t, args(thisChain), args(thisChain))
}

// A 0x00 inside the binary fields cannot make two different recoveries produce the same bytes.
func TestRecoveryMessages_BinaryFieldsCannotCollide(t *testing.T) {
	require.NotEqual(t,
		types.SocialRecoveryPoPMessage(thisChain, "did:phi:abc", []byte("key\x00ctrl"), "", []byte("n")),
		types.SocialRecoveryPoPMessage(thisChain, "did:phi:abc", []byte("key"), "ctrl", []byte("n")))

	require.NotEqual(t,
		types.ReauthAttestationMessage(thisChain, "did:phi:abc", []byte("k"), "c", []byte("uniq\x00nonce"), []byte("")),
		types.ReauthAttestationMessage(thisChain, "did:phi:abc", []byte("k"), "c", []byte("uniq"), []byte("nonce")))
}

// Each message starts with its own framed domain, so no message of one family can be presented as another's even before its fields are read.
func TestRecoveryMessages_CarryTheirFramedDomain(t *testing.T) {
	pop := types.SocialRecoveryPoPMessage(thisChain, "did:phi:abc", []byte("k"), "c", []byte("n"))
	att := types.ReauthAttestationMessage(thisChain, "did:phi:abc", []byte("k"), "c", []byte("u"), []byte("n"))

	require.Equal(t, types.CanonicalMessage(types.SocialRecoveryPoPDomain), pop[:len(types.SocialRecoveryPoPDomain)+4])
	require.Equal(t, types.CanonicalMessage(types.ReauthRecoveryDomain), att[:len(types.ReauthRecoveryDomain)+4])
}
