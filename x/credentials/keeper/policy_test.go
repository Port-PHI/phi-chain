// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/phicrypto"
	"github.com/Port-PHI/phi-chain/x/credentials/types"
)

// Registering a template records the policy and derives its chain-computed hash.
func TestPolicy_RegisteredWithTheTemplate(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(issuerDID, acc(issuerAddr))
	f.ident.addActive(aliceDID, acc(aliceAddr))
	f.registerTemplate(t, "phi.kyc.v1")

	tmpl, found := f.k.GetTemplate(f.ctx, "phi.kyc.v1")
	require.True(t, found)
	require.Equal(t, uint32(4), tmpl.MessageCount)
	require.Equal(t, []uint32{1, 2}, tmpl.DisclosableIndices)
	require.Equal(t, types.DisclosurePolicyHash("phi.kyc.v1", 1, 4, []uint32{1, 2}), tmpl.DisclosurePolicyHash)
	require.Equal(t, tmpl.PolicyHash(), tmpl.DisclosurePolicyHash, "the stored hash always describes the stored policy")
}

// The hash is canonical: order and repeats do not matter; distinct policies never collide.
func TestPolicy_HashIsCanonical(t *testing.T) {
	base := types.DisclosurePolicyHash("phi.kyc.v1", 1, 4, []uint32{1, 2})

	require.Equal(t, base, types.DisclosurePolicyHash("phi.kyc.v1", 1, 4, []uint32{2, 1}), "order does not matter")
	require.Equal(t, base, types.DisclosurePolicyHash("phi.kyc.v1", 1, 4, []uint32{2, 1, 2}), "repeats do not matter")

	require.NotEqual(t, base, types.DisclosurePolicyHash("phi.kyc.v1", 2, 4, []uint32{1, 2}), "version binds")
	require.NotEqual(t, base, types.DisclosurePolicyHash("phi.kyc.v2", 1, 4, []uint32{1, 2}), "template id binds")
	require.NotEqual(t, base, types.DisclosurePolicyHash("phi.kyc.v1", 1, 6, []uint32{1, 2}), "claim count binds")
	require.NotEqual(t, base, types.DisclosurePolicyHash("phi.kyc.v1", 1, 4, []uint32{1, 2, 3}), "the disclosable set binds")
	require.NotEqual(t, base, types.DisclosurePolicyHash("phi.kyc.v1", 1, 4, nil), "narrowing to nothing binds")
}

// An incoherent policy is rejected at registration.
func TestPolicy_IncoherentPolicyRejected(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(issuerDID, acc(issuerAddr))

	mk := func(messageCount uint32, disclosable []uint32) error {
		_, err := f.msg.RegisterCredentialTemplate(f.ctx, &types.MsgRegisterCredentialTemplate{
			Creator: acc(issuerAddr), Id: "tmpl-policy", OwnerDid: issuerDID, SchemaHash: []byte("s"), Name: "n",
			IssuerBbsPubkey: []byte("issuer-bbs-pubkey"),
			MessageCount:    messageCount, DisclosableIndices: disclosable,
		})
		return err
	}

	require.ErrorIs(t, mk(4, []uint32{4}), types.ErrInvalidRequest, "index past the end of the credential")
	require.ErrorIs(t, mk(4, []uint32{1, 1}), types.ErrInvalidRequest, "duplicate index")
	require.ErrorIs(t, mk(0, []uint32{0}), types.ErrInvalidRequest, "disclosable claims but no claims")
	require.ErrorIs(t, mk(types.MaxBbsMessages+1, nil), types.ErrInvalidRequest, "more claims than BBS+ can sign")

	require.NoError(t, mk(4, nil))
	f.registerTemplate(t, "phi.open.v1")
}

// Updating a template bumps the version and re-derives the policy hash, invalidating an old pin.
func TestPolicy_UpdateBumpsVersionAndRebindsTheHash(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(issuerDID, acc(issuerAddr))
	f.ident.addActive(aliceDID, acc(aliceAddr))
	f.registerTemplate(t, "phi.kyc.v1")

	before, _ := f.k.GetTemplate(f.ctx, "phi.kyc.v1")
	require.Equal(t, uint32(1), before.Version)

	res, err := f.msg.UpdateCredentialTemplate(f.ctx, &types.MsgUpdateCredentialTemplate{
		Creator: acc(issuerAddr), Id: "phi.kyc.v1", SchemaHash: []byte("schema-v2"), Name: "KYC v2",
		MessageCount: 4, DisclosableIndices: []uint32{1, 2, 3},
	})
	require.NoError(t, err)
	require.Equal(t, uint32(2), res.Version)

	after, _ := f.k.GetTemplate(f.ctx, "phi.kyc.v1")
	require.Equal(t, uint32(2), after.Version)
	require.Equal(t, []uint32{1, 2, 3}, after.DisclosableIndices)
	require.Equal(t, after.PolicyHash(), after.DisclosurePolicyHash)
	require.Equal(t, after.DisclosurePolicyHash, res.DisclosurePolicyHash, "the response hands back the new pin")
	require.NotEqual(t, before.DisclosurePolicyHash, after.DisclosurePolicyHash,
		"a changed policy MUST invalidate a pinned hash")

	require.Equal(t, before.IssuerBbsPubkey, after.IssuerBbsPubkey)

	_, err = f.msg.UpdateCredentialTemplate(f.ctx, &types.MsgUpdateCredentialTemplate{
		Creator: acc(issuerAddr), Id: "phi.kyc.v1", SchemaHash: []byte("schema-v3"), Name: "KYC v3",
		MessageCount: 4, DisclosableIndices: nil,
	})
	require.NoError(t, err)
	narrowed, _ := f.k.GetTemplate(f.ctx, "phi.kyc.v1")
	require.Empty(t, narrowed.DisclosableIndices, "nothing may be revealed any more")
	require.Equal(t, uint32(3), narrowed.Version)
}

// Only the template's controller may set its policy.
func TestPolicy_OnlyTheOwnerMaySetIt(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(issuerDID, acc(issuerAddr))
	f.ident.addActive(aliceDID, acc(aliceAddr))
	f.registerTemplate(t, "phi.kyc.v1")

	_, err := f.msg.UpdateCredentialTemplate(f.ctx, &types.MsgUpdateCredentialTemplate{
		Creator: acc(aliceAddr), Id: "phi.kyc.v1", SchemaHash: []byte("s"), Name: "n",
		MessageCount: 4, DisclosableIndices: []uint32{0, 1, 2, 3}, // a stranger trying to open the credential up
	})
	require.Error(t, err)

	tmpl, _ := f.k.GetTemplate(f.ctx, "phi.kyc.v1")
	require.Equal(t, []uint32{1, 2}, tmpl.DisclosableIndices, "the policy is untouched")
	require.Equal(t, uint32(1), tmpl.Version)
}

// A deprecated template's policy is frozen and cannot be updated.
func TestPolicy_DeprecatedTemplateCannotBeRepolicied(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(issuerDID, acc(issuerAddr))
	f.ident.addActive(aliceDID, acc(aliceAddr))
	f.registerTemplate(t, "phi.kyc.v1")

	_, err := f.msg.DeprecateCredentialTemplate(f.ctx, &types.MsgDeprecateCredentialTemplate{
		Creator: acc(issuerAddr), Id: "phi.kyc.v1",
	})
	require.NoError(t, err)

	_, err = f.msg.UpdateCredentialTemplate(f.ctx, &types.MsgUpdateCredentialTemplate{
		Creator: acc(issuerAddr), Id: "phi.kyc.v1", SchemaHash: []byte("s"), Name: "n",
		MessageCount: 4, DisclosableIndices: []uint32{0, 1, 2, 3},
	})
	require.ErrorIs(t, err, types.ErrTemplateDeprecated)

	tmpl, _ := f.k.GetTemplate(f.ctx, "phi.kyc.v1")
	require.Equal(t, []uint32{1, 2}, tmpl.DisclosableIndices)
}

// Genesis validation recomputes the policy hash and refuses a mismatch.
func TestPolicy_GenesisBindsTheHashToThePolicy(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(issuerDID, acc(issuerAddr))
	f.ident.addActive(aliceDID, acc(aliceAddr))
	f.registerTemplate(t, "phi.kyc.v1")

	exported := f.k.ExportGenesis(f.ctx)
	require.NoError(t, exported.Validate())
	require.Equal(t, []uint32{1, 2}, exported.Templates[0].DisclosableIndices, "the policy round-trips")

	forged := *exported
	forged.Templates = append([]types.CredentialTemplate{}, exported.Templates...)
	forged.Templates[0].DisclosableIndices = []uint32{0, 1, 2, 3} // widened, but the hash left untouched
	require.Error(t, forged.Validate(), "a policy hash that does not match its policy must not import")

	broken := *exported
	broken.Templates = append([]types.CredentialTemplate{}, exported.Templates...)
	broken.Templates[0].DisclosableIndices = []uint32{9}
	broken.Templates[0].DisclosurePolicyHash = broken.Templates[0].PolicyHash() // hash matches, policy is nonsense
	require.Error(t, broken.Validate(), "an index the credential does not have must not import")
}
