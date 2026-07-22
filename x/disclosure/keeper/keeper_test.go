// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"encoding/binary"
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/phicrypto"
	credentialstypes "github.com/Port-PHI/phi-chain/x/credentials/types"
	"github.com/Port-PHI/phi-chain/x/disclosure/keeper"
	"github.com/Port-PHI/phi-chain/x/disclosure/types"
)

type fakeCredentials struct {
	anchors   map[string]credentialstypes.CredentialAnchor
	templates map[string]credentialstypes.CredentialTemplate
}

func newFakeCredentials() *fakeCredentials {
	return &fakeCredentials{
		anchors:   map[string]credentialstypes.CredentialAnchor{},
		templates: map[string]credentialstypes.CredentialTemplate{},
	}
}

func (f *fakeCredentials) GetAnchor(_ sdk.Context, hash []byte) (credentialstypes.CredentialAnchor, bool) {
	a, ok := f.anchors[string(hash)]
	return a, ok
}

func (f *fakeCredentials) GetTemplate(_ sdk.Context, id string) (credentialstypes.CredentialTemplate, bool) {
	t, ok := f.templates[id]
	return t, ok
}

func (f *fakeCredentials) addTemplate(id string, bbsKey []byte) {
	f.addTemplateWithPolicy(id, bbsKey, 4, []uint32{1, 2})
}

func (f *fakeCredentials) addTemplateWithPolicy(id string, bbsKey []byte, messageCount uint32, disclosable []uint32) {
	t := credentialstypes.CredentialTemplate{
		Id: id, Version: 1, OwnerDid: "did:phi:issuer",
		IssuerBbsPubkey: bbsKey, Status: credentialstypes.TEMPLATE_STATUS_ACTIVE,
		MessageCount: messageCount, DisclosableIndices: disclosable,
	}
	t.DisclosurePolicyHash = t.PolicyHash()
	f.templates[id] = t
}

func proofEnvelope(messageCount uint32, revealed ...uint32) []byte {
	var out []byte
	be := func(v uint32) {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], v)
		out = append(out, b[:]...)
	}
	be(messageCount)
	body := []byte("bbs-proof-body")
	be(uint32(len(body)))
	out = append(out, body...)
	be(uint32(len(revealed)))
	for _, i := range revealed {
		be(i)
		claim := []byte("claim-value")
		be(uint32(len(claim)))
		out = append(out, claim...)
	}
	return out
}

func (f *fakeCredentials) addAnchor(hash []byte, templateID string, status credentialstypes.CredentialStatus) {
	f.anchors[string(hash)] = credentialstypes.CredentialAnchor{
		CredentialHash: hash, TemplateId: templateID, IssuerDid: "did:phi:issuer",
		SubjectDid: "did:phi:subject", Status: status,
	}
}

type fixture struct {
	ctx   sdk.Context
	k     keeper.Keeper
	msg   types.MsgServer
	creds *fakeCredentials
}

func setup(t *testing.T, verifier phicrypto.Verifier) fixture {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_disc"))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	creds := newFakeCredentials()
	authority := sdk.AccAddress([]byte("gov_authority_______")).String()

	k := keeper.NewKeeper(cdc, key, authority, creds, verifier)
	require.NoError(t, k.SetParams(testCtx.Ctx, types.DefaultParams()))

	return fixture{ctx: testCtx.Ctx, k: k, msg: keeper.NewMsgServerImpl(k), creds: creds}
}

const (
	credHash = "cred-hash-1"
	tmplID   = "phi.kyc.v1"
)

var bbsKey = []byte("issuer-bbs-pubkey-96-bytes")

func TestVerifyDisclosure_ValidProofActiveCredential(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.creds.addTemplate(tmplID, bbsKey)
	f.creds.addAnchor([]byte(credHash), tmplID, credentialstypes.CREDENTIAL_STATUS_ACTIVE)

	res, err := f.k.VerifyDisclosure(f.ctx, &types.QueryVerifyDisclosureRequest{
		CredentialHash: []byte(credHash), Proof: proofEnvelope(4, 1), Nonce: []byte("nonce"),
	})
	require.NoError(t, err)
	require.True(t, res.Valid)
	require.Equal(t, "did:phi:issuer", res.IssuerDid)
	require.Equal(t, tmplID, res.TemplateId)
	require.Equal(t, types.DISCLOSURE_LEVEL_PARTIAL, res.Level)
	require.Equal(t, []uint32{1}, res.RevealedIndices)
}

func TestVerifyDisclosure_BadProofRejected(t *testing.T) {
	f := setup(t, phicrypto.RejectAll())
	f.creds.addTemplate(tmplID, bbsKey)
	f.creds.addAnchor([]byte(credHash), tmplID, credentialstypes.CREDENTIAL_STATUS_ACTIVE)

	res, err := f.k.VerifyDisclosure(f.ctx, &types.QueryVerifyDisclosureRequest{
		CredentialHash: []byte(credHash), Proof: proofEnvelope(4, 1), Nonce: []byte("nonce"),
	})
	require.NoError(t, err)
	require.False(t, res.Valid)
	require.Contains(t, res.Reason, "proof verification failed")
}

func TestVerifyDisclosure_RevokedCredentialRejected(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.creds.addTemplate(tmplID, bbsKey)
	f.creds.addAnchor([]byte(credHash), tmplID, credentialstypes.CREDENTIAL_STATUS_REVOKED)

	res, err := f.k.VerifyDisclosure(f.ctx, &types.QueryVerifyDisclosureRequest{
		CredentialHash: []byte(credHash), Proof: proofEnvelope(4, 1), Nonce: []byte("nonce"),
	})
	require.NoError(t, err)
	require.False(t, res.Valid)
	require.Contains(t, res.Reason, "revoked")
}

func TestVerifyDisclosure_UnknownCredentialRejected(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.creds.addTemplate(tmplID, bbsKey)

	res, err := f.k.VerifyDisclosure(f.ctx, &types.QueryVerifyDisclosureRequest{
		CredentialHash: []byte(credHash), Proof: proofEnvelope(4, 1), Nonce: []byte("nonce"),
	})
	require.NoError(t, err)
	require.False(t, res.Valid)
	require.Contains(t, res.Reason, "not anchored")
}

func TestVerifyDisclosure_NoIssuerKeyRejected(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.creds.addTemplate(tmplID, nil) // template without a BBS key
	f.creds.addAnchor([]byte(credHash), tmplID, credentialstypes.CREDENTIAL_STATUS_ACTIVE)

	res, err := f.k.VerifyDisclosure(f.ctx, &types.QueryVerifyDisclosureRequest{
		CredentialHash: []byte(credHash), Proof: proofEnvelope(4, 1), Nonce: []byte("nonce"),
	})
	require.NoError(t, err)
	require.False(t, res.Valid)
	require.Contains(t, res.Reason, "no issuer BBS public key")
}

func TestVerifyDisclosure_MissingTemplateRejected(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.creds.addAnchor([]byte(credHash), "ghost", credentialstypes.CREDENTIAL_STATUS_ACTIVE)

	res, err := f.k.VerifyDisclosure(f.ctx, &types.QueryVerifyDisclosureRequest{
		CredentialHash: []byte(credHash), Proof: proofEnvelope(4, 1), Nonce: []byte("nonce"),
	})
	require.NoError(t, err)
	require.False(t, res.Valid)
	require.Contains(t, res.Reason, "template not found")
}

func TestVerifyDisclosure_ProofTooLargeRejected(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	require.NoError(t, f.k.SetParams(f.ctx, types.Params{MaxProofSizeBytes: 4}))
	f.creds.addTemplate(tmplID, bbsKey)
	f.creds.addAnchor([]byte(credHash), tmplID, credentialstypes.CREDENTIAL_STATUS_ACTIVE)

	res, err := f.k.VerifyDisclosure(f.ctx, &types.QueryVerifyDisclosureRequest{
		CredentialHash: []byte(credHash), Proof: []byte("this-proof-is-too-long"), Nonce: []byte("nonce"),
	})
	require.NoError(t, err)
	require.False(t, res.Valid)
	require.Contains(t, res.Reason, "max_proof_size_bytes")
}

func TestVerifyDisclosure_EmptyInputErrors(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())

	_, err := f.k.VerifyDisclosure(f.ctx, &types.QueryVerifyDisclosureRequest{
		CredentialHash: nil, Proof: []byte("proof"),
	})
	require.Error(t, err)

	_, err = f.k.VerifyDisclosure(f.ctx, &types.QueryVerifyDisclosureRequest{
		CredentialHash: []byte(credHash), Proof: nil,
	})
	require.Error(t, err)
}

func TestUpdateParams_OnlyAuthority(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())

	_, err := f.msg.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority: sdk.AccAddress([]byte("not_the_authority___")).String(),
		Params:    types.Params{MaxProofSizeBytes: 1024},
	})
	require.Error(t, err)

	_, err = f.msg.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority: f.k.GetAuthority(), Params: types.Params{MaxProofSizeBytes: 1024},
	})
	require.NoError(t, err)
	require.Equal(t, uint32(1024), f.k.GetParams(f.ctx).MaxProofSizeBytes)
}
