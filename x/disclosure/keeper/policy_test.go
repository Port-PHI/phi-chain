// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/phicrypto"
	credentialstypes "github.com/Port-PHI/phi-chain/x/credentials/types"
	"github.com/Port-PHI/phi-chain/x/disclosure/types"
)

// L2 — a partial reveal, entirely within policy, is accepted, and the chain reports exactly what was revealed rather than merely "valid".
func TestPolicy_DisclosableFieldsAccepted(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.creds.addTemplate(tmplID, bbsKey)
	f.creds.addAnchor([]byte(credHash), tmplID, credentialstypes.CREDENTIAL_STATUS_ACTIVE)

	res, err := f.k.VerifyDisclosure(f.ctx, &types.QueryVerifyDisclosureRequest{
		CredentialHash: []byte(credHash), Proof: proofEnvelope(4, 2, 1), Nonce: []byte("nonce"),
	})
	require.NoError(t, err)
	require.True(t, res.Valid)
	require.Equal(t, types.DISCLOSURE_LEVEL_PARTIAL, res.Level, "L2 — a partial reveal")
	require.Equal(t, []uint32{1, 2}, res.RevealedIndices, "reported ascending, whatever order the proof listed")
}

// THE CORE GUARANTEE.
func TestPolicy_NonDisclosableFieldRejected(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.creds.addTemplate(tmplID, bbsKey)
	f.creds.addAnchor([]byte(credHash), tmplID, credentialstypes.CREDENTIAL_STATUS_ACTIVE)

	for _, forbidden := range []uint32{0, 3} {
		res, err := f.k.VerifyDisclosure(f.ctx, &types.QueryVerifyDisclosureRequest{
			CredentialHash: []byte(credHash), Proof: proofEnvelope(4, forbidden), Nonce: []byte("nonce"),
		})
		require.NoError(t, err)
		require.False(t, res.Valid, "claim %d is not disclosable and must never verify", forbidden)
		require.Contains(t, res.Reason, "not disclosable")
		require.Empty(t, res.RevealedIndices)
	}

	res, err := f.k.VerifyDisclosure(f.ctx, &types.QueryVerifyDisclosureRequest{
		CredentialHash: []byte(credHash), Proof: proofEnvelope(4, 1, 2, 3), Nonce: []byte("nonce"),
	})
	require.NoError(t, err)
	require.False(t, res.Valid)
	require.Contains(t, res.Reason, "not disclosable")
}

// L1 — a predicate / zero-knowledge proof reveals no raw claim, so there is nothing for the policy to forbid: it is accepted even under a template that discloses NOTHING.
func TestPolicy_PredicateProofRevealsNothingAndIsAccepted(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.creds.addTemplate(tmplID, bbsKey)
	f.creds.addAnchor([]byte(credHash), tmplID, credentialstypes.CREDENTIAL_STATUS_ACTIVE)

	res, err := f.k.VerifyDisclosure(f.ctx, &types.QueryVerifyDisclosureRequest{
		CredentialHash: []byte(credHash), Proof: proofEnvelope(4), Nonce: []byte("nonce"),
	})
	require.NoError(t, err)
	require.True(t, res.Valid)
	require.Equal(t, types.DISCLOSURE_LEVEL_PREDICATE, res.Level, "L1 — nothing revealed")
	require.Empty(t, res.RevealedIndices)

	f.creds.addTemplateWithPolicy("phi.zk-only.v1", bbsKey, 4, nil)
	f.creds.addAnchor([]byte("zk-cred"), "phi.zk-only.v1", credentialstypes.CREDENTIAL_STATUS_ACTIVE)

	res, err = f.k.VerifyDisclosure(f.ctx, &types.QueryVerifyDisclosureRequest{
		CredentialHash: []byte("zk-cred"), Proof: proofEnvelope(4), Nonce: []byte("nonce"),
	})
	require.NoError(t, err)
	require.True(t, res.Valid)
	require.Equal(t, types.DISCLOSURE_LEVEL_PREDICATE, res.Level)

	res, err = f.k.VerifyDisclosure(f.ctx, &types.QueryVerifyDisclosureRequest{
		CredentialHash: []byte("zk-cred"), Proof: proofEnvelope(4, 1), Nonce: []byte("nonce"),
	})
	require.NoError(t, err)
	require.False(t, res.Valid, "a template with no disclosable claims discloses nothing")
	require.Contains(t, res.Reason, "not disclosable")
}

// THE POLICY HASH BINDS.
func TestPolicy_PolicyHashBindsTheVersion(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.creds.addTemplate(tmplID, bbsKey)
	f.creds.addAnchor([]byte(credHash), tmplID, credentialstypes.CREDENTIAL_STATUS_ACTIVE)

	tmpl, ok := f.creds.GetTemplate(f.ctx, tmplID)
	require.True(t, ok)
	audited := tmpl.DisclosurePolicyHash
	require.NotEmpty(t, audited)

	res, err := f.k.VerifyDisclosure(f.ctx, &types.QueryVerifyDisclosureRequest{
		CredentialHash: []byte(credHash), Proof: proofEnvelope(4, 1), Nonce: []byte("nonce"),
		DisclosurePolicyHash: audited,
	})
	require.NoError(t, err)
	require.True(t, res.Valid)
	require.Equal(t, audited, res.DisclosurePolicyHash)

	f.creds.addTemplateWithPolicy(tmplID, bbsKey, 4, []uint32{1, 2, 3})
	updated, _ := f.creds.GetTemplate(f.ctx, tmplID)
	require.NotEqual(t, audited, updated.DisclosurePolicyHash, "changing the policy changes its hash")

	res, err = f.k.VerifyDisclosure(f.ctx, &types.QueryVerifyDisclosureRequest{
		CredentialHash: []byte(credHash), Proof: proofEnvelope(4, 1), Nonce: []byte("nonce"),
		DisclosurePolicyHash: audited, // what the verifier audited — no longer what the chain enforces
	})
	require.NoError(t, err)
	require.False(t, res.Valid)
	require.Contains(t, res.Reason, "policy hash mismatch")

	res, err = f.k.VerifyDisclosure(f.ctx, &types.QueryVerifyDisclosureRequest{
		CredentialHash: []byte(credHash), Proof: proofEnvelope(4, 1), Nonce: []byte("nonce"),
		DisclosurePolicyHash: []byte("not-a-policy-hash"),
	})
	require.NoError(t, err)
	require.False(t, res.Valid)
	require.Contains(t, res.Reason, "policy hash mismatch")
}

// A proof over a credential of a different SHAPE is refused: index 3 of a 6-claim credential is not index 3 of this template's 4-claim one, and letting the counts differ would let a policy be enforced against the wrong fields entirely.
func TestPolicy_MessageCountMustMatchTheTemplate(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.creds.addTemplate(tmplID, bbsKey)
	f.creds.addAnchor([]byte(credHash), tmplID, credentialstypes.CREDENTIAL_STATUS_ACTIVE)

	res, err := f.k.VerifyDisclosure(f.ctx, &types.QueryVerifyDisclosureRequest{
		CredentialHash: []byte(credHash), Proof: proofEnvelope(6, 1), Nonce: []byte("nonce"),
	})
	require.NoError(t, err)
	require.False(t, res.Valid)
	require.Contains(t, res.Reason, "message_count")
}

// The policy is enforced INDEPENDENTLY of the cryptography, and both must pass.
func TestPolicy_CryptoAndPolicyAreBothRequired(t *testing.T) {
	f := setup(t, phicrypto.RejectAll())
	f.creds.addTemplate(tmplID, bbsKey)
	f.creds.addAnchor([]byte(credHash), tmplID, credentialstypes.CREDENTIAL_STATUS_ACTIVE)

	res, err := f.k.VerifyDisclosure(f.ctx, &types.QueryVerifyDisclosureRequest{
		CredentialHash: []byte(credHash), Proof: proofEnvelope(4, 1), Nonce: []byte("nonce"),
	})
	require.NoError(t, err)
	require.False(t, res.Valid)
	require.Contains(t, res.Reason, "proof verification failed")
	require.Equal(t, types.DISCLOSURE_LEVEL_UNSPECIFIED, res.Level, "a failed proof exercised no level")
}

// A malformed envelope fails CLOSED, even under an accept-everything verifier.
func TestPolicy_MalformedEnvelopeFailsClosed(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.creds.addTemplate(tmplID, bbsKey)
	f.creds.addAnchor([]byte(credHash), tmplID, credentialstypes.CREDENTIAL_STATUS_ACTIVE)

	good := proofEnvelope(4, 1)
	cases := map[string][]byte{
		"truncated":                 good[:len(good)-3],
		"trailing bytes":            append(append([]byte{}, good...), 0x00),
		"garbage":                   []byte("not-an-envelope-at-all"),
		"zero message_count":        proofEnvelope(0),
		"count above the max":       proofEnvelope(types.MaxBbsMessages + 1),
		"index out of range":        proofEnvelope(4, 9),
		"same index revealed twice": proofEnvelope(4, 1, 1),
	}
	for name, proof := range cases {
		res, err := f.k.VerifyDisclosure(f.ctx, &types.QueryVerifyDisclosureRequest{
			CredentialHash: []byte(credHash), Proof: proof, Nonce: []byte("nonce"),
		})
		require.NoError(t, err, name)
		require.False(t, res.Valid, "%s must not verify", name)
	}
}
