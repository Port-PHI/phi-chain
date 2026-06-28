// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"testing"
	"time"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/phicrypto"
	"github.com/Port-PHI/phi-chain/x/credentials/keeper"
	"github.com/Port-PHI/phi-chain/x/credentials/types"
	identitytypes "github.com/Port-PHI/phi-chain/x/identity/types"
)

// fakeIdentity is an in-memory x/identity stub for tests.
type fakeIdentity struct {
	docs map[string]identitytypes.DIDDocument
}

func newFakeIdentity() *fakeIdentity {
	return &fakeIdentity{docs: map[string]identitytypes.DIDDocument{}}
}

func (f *fakeIdentity) GetIdentity(_ sdk.Context, did string) (identitytypes.DIDDocument, bool) {
	d, ok := f.docs[did]
	return d, ok
}

func (f *fakeIdentity) addActive(did, controller string) {
	f.docs[did] = identitytypes.DIDDocument{
		Did:        did,
		Controller: controller,
		PubKey:     []byte("pk-" + did),
		Status:     identitytypes.DID_STATUS_ACTIVE,
	}
}

func (f *fakeIdentity) addRevoked(did, controller string) {
	f.docs[did] = identitytypes.DIDDocument{
		Did:        did,
		Controller: controller,
		PubKey:     []byte("pk-" + did),
		Status:     identitytypes.DID_STATUS_REVOKED,
	}
}

type fixture struct {
	ctx   sdk.Context
	k     keeper.Keeper
	msg   types.MsgServer
	ident *fakeIdentity
}

func acc(s string) string { return sdk.AccAddress([]byte(s)).String() }

const (
	issuerAddr  = "issuer______________"
	subjectAddr = "subject_____________"
	aliceAddr   = "alice_______________"
	bobAddr     = "bob_________________"

	issuerDID  = "did:phi:issuer"
	subjectDID = "did:phi:subject"
	aliceDID   = "did:phi:alice"
	bobDID     = "did:phi:bob"
)

func setup(t *testing.T, verifier phicrypto.Verifier) fixture {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_cred"))
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	ident := newFakeIdentity()
	authority := sdk.AccAddress([]byte("gov_authority_______")).String()

	k := keeper.NewKeeper(cdc, key, authority, ident, verifier)
	ctx := testCtx.Ctx.WithBlockTime(time.Unix(1_000_000, 0))
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	return fixture{ctx: ctx, k: k, msg: keeper.NewMsgServerImpl(k), ident: ident}
}

// registerTemplate is a helper that registers an active template owned by issuerDID.
func (f fixture) registerTemplate(t *testing.T, id string) {
	t.Helper()
	_, err := f.msg.RegisterCredentialTemplate(f.ctx, &types.MsgRegisterCredentialTemplate{
		Creator:         acc(issuerAddr),
		Id:              id,
		OwnerDid:        issuerDID,
		SchemaHash:      []byte("schema-v1"),
		Name:            "KYC",
		IssuerBbsPubkey: []byte("issuer-bbs-pubkey"),
	})
	require.NoError(t, err)
}

// TestRegisterTemplate_RejectsBadBbsKey covers the case where the issuer BBS public key must be present
// and length-bounded.
func TestRegisterTemplate_RejectsBadBbsKey(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(issuerDID, acc(issuerAddr))
	mk := func(key []byte) error {
		_, err := f.msg.RegisterCredentialTemplate(f.ctx, &types.MsgRegisterCredentialTemplate{
			Creator: acc(issuerAddr), Id: "tmpl-x", OwnerDid: issuerDID, SchemaHash: []byte("s"), Name: "n", IssuerBbsPubkey: key,
		})
		return err
	}
	require.ErrorIs(t, mk(nil), types.ErrInvalidRequest, "an empty BBS key must be rejected")
	require.ErrorIs(t, mk(make([]byte, types.MaxBbsPubkeyLen+1)), types.ErrInvalidRequest, "an over-long BBS key must be rejected")
	require.NoError(t, mk([]byte("issuer-bbs-pubkey")), "a bounded key is accepted")
}

// --- templates ---

func TestTemplate_Lifecycle(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(issuerDID, acc(issuerAddr))

	f.registerTemplate(t, "phi.kyc.v1")
	tmpl, ok := f.k.GetTemplate(f.ctx, "phi.kyc.v1")
	require.True(t, ok)
	require.Equal(t, uint32(1), tmpl.Version)
	require.Equal(t, types.TEMPLATE_STATUS_ACTIVE, tmpl.Status)

	// duplicate id is rejected
	_, err := f.msg.RegisterCredentialTemplate(f.ctx, &types.MsgRegisterCredentialTemplate{
		Creator: acc(issuerAddr), Id: "phi.kyc.v1", OwnerDid: issuerDID, SchemaHash: []byte("x"),
	})
	require.ErrorIs(t, err, types.ErrTemplateExists)

	// update bumps the version and changes the schema hash
	res, err := f.msg.UpdateCredentialTemplate(f.ctx, &types.MsgUpdateCredentialTemplate{
		Creator: acc(issuerAddr), Id: "phi.kyc.v1", SchemaHash: []byte("schema-v2"), Name: "KYC2",
	})
	require.NoError(t, err)
	require.Equal(t, uint32(2), res.Version)
	tmpl, _ = f.k.GetTemplate(f.ctx, "phi.kyc.v1")
	require.Equal(t, []byte("schema-v2"), tmpl.SchemaHash)
}

func TestTemplate_BbsKeyImmutableAcrossUpdate(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(issuerDID, acc(issuerAddr))

	bbsKey := []byte("issuer-bbs-pubkey")
	_, err := f.msg.RegisterCredentialTemplate(f.ctx, &types.MsgRegisterCredentialTemplate{
		Creator: acc(issuerAddr), Id: "phi.kyc.v1", OwnerDid: issuerDID,
		SchemaHash: []byte("schema-v1"), Name: "KYC", IssuerBbsPubkey: bbsKey,
	})
	require.NoError(t, err)

	// An update that changes the schema must NOT clear or change the BBS key
	// (the key is immutable after registration).
	_, err = f.msg.UpdateCredentialTemplate(f.ctx, &types.MsgUpdateCredentialTemplate{
		Creator: acc(issuerAddr), Id: "phi.kyc.v1", SchemaHash: []byte("schema-v2"), Name: "KYC2",
	})
	require.NoError(t, err)

	tmpl, ok := f.k.GetTemplate(f.ctx, "phi.kyc.v1")
	require.True(t, ok)
	require.Equal(t, uint32(2), tmpl.Version)
	require.Equal(t, []byte("schema-v2"), tmpl.SchemaHash)
	require.Equal(t, bbsKey, tmpl.IssuerBbsPubkey, "BBS key must be immutable across updates")
}

func TestTemplate_UpdateRequiresOwner(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(issuerDID, acc(issuerAddr))
	f.ident.addActive(aliceDID, acc(aliceAddr))
	f.registerTemplate(t, "phi.kyc.v1")

	// alice controls aliceDID, not the template owner issuerDID → unauthorized
	_, err := f.msg.UpdateCredentialTemplate(f.ctx, &types.MsgUpdateCredentialTemplate{
		Creator: acc(aliceAddr), Id: "phi.kyc.v1", SchemaHash: []byte("y"),
	})
	require.ErrorIs(t, err, types.ErrUnauthorized)
}

func TestTemplate_DeprecateRejectsNewAnchors(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(issuerDID, acc(issuerAddr))
	f.ident.addActive(subjectDID, acc(subjectAddr))
	f.registerTemplate(t, "phi.kyc.v1")

	_, err := f.msg.DeprecateCredentialTemplate(f.ctx, &types.MsgDeprecateCredentialTemplate{
		Creator: acc(issuerAddr), Id: "phi.kyc.v1",
	})
	require.NoError(t, err)

	_, err = f.msg.AnchorCredential(f.ctx, &types.MsgAnchorCredential{
		Issuer: acc(issuerAddr), IssuerDid: issuerDID, SubjectDid: subjectDID,
		TemplateId: "phi.kyc.v1", TemplateVersion: 1, CredentialHash: []byte("cred-1"), IssuerSig: []byte("sig"),
	})
	require.ErrorIs(t, err, types.ErrTemplateDeprecated)
}

// --- credential anchors ---

func TestAnchorCredential_GoodSignature(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(issuerDID, acc(issuerAddr))
	f.ident.addActive(subjectDID, acc(subjectAddr))
	f.registerTemplate(t, "phi.kyc.v1")

	_, err := f.msg.AnchorCredential(f.ctx, &types.MsgAnchorCredential{
		Issuer: acc(issuerAddr), IssuerDid: issuerDID, SubjectDid: subjectDID,
		TemplateId: "phi.kyc.v1", TemplateVersion: 1, CredentialHash: []byte("cred-1"), IssuerSig: []byte("sig"),
	})
	require.NoError(t, err)

	a, ok := f.k.GetAnchor(f.ctx, []byte("cred-1"))
	require.True(t, ok)
	require.Equal(t, types.CREDENTIAL_STATUS_ACTIVE, a.Status)
	require.Equal(t, subjectDID, a.SubjectDid)
}

func TestAnchorCredential_BadSignatureRejected(t *testing.T) {
	f := setup(t, phicrypto.RejectAll())
	f.ident.addActive(issuerDID, acc(issuerAddr))
	f.ident.addActive(subjectDID, acc(subjectAddr))
	f.registerTemplate(t, "phi.kyc.v1")

	_, err := f.msg.AnchorCredential(f.ctx, &types.MsgAnchorCredential{
		Issuer: acc(issuerAddr), IssuerDid: issuerDID, SubjectDid: subjectDID,
		TemplateId: "phi.kyc.v1", TemplateVersion: 1, CredentialHash: []byte("cred-1"), IssuerSig: []byte("bad"),
	})
	require.ErrorIs(t, err, types.ErrInvalidSignature)
	require.False(t, f.k.HasAnchor(f.ctx, []byte("cred-1")))
}

func TestAnchorCredential_VersionMismatch(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(issuerDID, acc(issuerAddr))
	f.ident.addActive(subjectDID, acc(subjectAddr))
	f.registerTemplate(t, "phi.kyc.v1")

	_, err := f.msg.AnchorCredential(f.ctx, &types.MsgAnchorCredential{
		Issuer: acc(issuerAddr), IssuerDid: issuerDID, SubjectDid: subjectDID,
		TemplateId: "phi.kyc.v1", TemplateVersion: 2, CredentialHash: []byte("cred-1"), IssuerSig: []byte("sig"),
	})
	require.ErrorIs(t, err, types.ErrTemplateVersionMismatch)
}

func TestAnchorCredential_UnknownTemplate(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(issuerDID, acc(issuerAddr))
	f.ident.addActive(subjectDID, acc(subjectAddr))

	_, err := f.msg.AnchorCredential(f.ctx, &types.MsgAnchorCredential{
		Issuer: acc(issuerAddr), IssuerDid: issuerDID, SubjectDid: subjectDID,
		TemplateId: "missing", TemplateVersion: 1, CredentialHash: []byte("cred-1"), IssuerSig: []byte("sig"),
	})
	require.ErrorIs(t, err, types.ErrTemplateNotFound)
}

func TestAnchorCredential_IssuerMustControlDID(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(issuerDID, acc(issuerAddr))
	f.ident.addActive(subjectDID, acc(subjectAddr))
	f.registerTemplate(t, "phi.kyc.v1")

	// bob signs but issuerDID is controlled by issuerAddr → unauthorized
	_, err := f.msg.AnchorCredential(f.ctx, &types.MsgAnchorCredential{
		Issuer: acc(bobAddr), IssuerDid: issuerDID, SubjectDid: subjectDID,
		TemplateId: "phi.kyc.v1", TemplateVersion: 1, CredentialHash: []byte("cred-1"), IssuerSig: []byte("sig"),
	})
	require.ErrorIs(t, err, types.ErrUnauthorized)
}

func TestAnchorCredential_InactiveSubjectRejected(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(issuerDID, acc(issuerAddr))
	f.ident.addRevoked(subjectDID, acc(subjectAddr))
	f.registerTemplate(t, "phi.kyc.v1")

	_, err := f.msg.AnchorCredential(f.ctx, &types.MsgAnchorCredential{
		Issuer: acc(issuerAddr), IssuerDid: issuerDID, SubjectDid: subjectDID,
		TemplateId: "phi.kyc.v1", TemplateVersion: 1, CredentialHash: []byte("cred-1"), IssuerSig: []byte("sig"),
	})
	require.ErrorIs(t, err, types.ErrDIDNotActive)
}

func TestRevokeCredential(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(issuerDID, acc(issuerAddr))
	f.ident.addActive(subjectDID, acc(subjectAddr))
	f.registerTemplate(t, "phi.kyc.v1")
	_, err := f.msg.AnchorCredential(f.ctx, &types.MsgAnchorCredential{
		Issuer: acc(issuerAddr), IssuerDid: issuerDID, SubjectDid: subjectDID,
		TemplateId: "phi.kyc.v1", TemplateVersion: 1, CredentialHash: []byte("cred-1"), IssuerSig: []byte("sig"),
	})
	require.NoError(t, err)

	_, err = f.msg.RevokeCredential(f.ctx, &types.MsgRevokeCredential{
		Issuer: acc(issuerAddr), CredentialHash: []byte("cred-1"),
	})
	require.NoError(t, err)
	a, _ := f.k.GetAnchor(f.ctx, []byte("cred-1"))
	require.Equal(t, types.CREDENTIAL_STATUS_REVOKED, a.Status)

	// second revoke is rejected
	_, err = f.msg.RevokeCredential(f.ctx, &types.MsgRevokeCredential{
		Issuer: acc(issuerAddr), CredentialHash: []byte("cred-1"),
	})
	require.ErrorIs(t, err, types.ErrCredentialRevoked)
}

// --- agreements ---

func TestAgreement_FullSigningCompletes(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(aliceDID, acc(aliceAddr))
	f.ident.addActive(bobDID, acc(bobAddr))

	_, err := f.msg.CreateAgreement(f.ctx, &types.MsgCreateAgreement{
		Creator: acc(aliceAddr), Hash: []byte("agr-1"), RequiredSigners: []string{aliceDID, bobDID},
	})
	require.NoError(t, err)

	res, err := f.msg.SignAgreement(f.ctx, &types.MsgSignAgreement{
		Signer: acc(aliceAddr), Hash: []byte("agr-1"), SignerDid: aliceDID, Signature: []byte("s"),
	})
	require.NoError(t, err)
	require.False(t, res.Completed)

	res, err = f.msg.SignAgreement(f.ctx, &types.MsgSignAgreement{
		Signer: acc(bobAddr), Hash: []byte("agr-1"), SignerDid: bobDID, Signature: []byte("s"),
	})
	require.NoError(t, err)
	require.True(t, res.Completed)

	ag, _ := f.k.GetAgreement(f.ctx, []byte("agr-1"))
	require.Equal(t, types.AGREEMENT_STATUS_COMPLETED, ag.Status)
	require.Len(t, ag.Signatures, 2)
}

func TestAgreement_RejectsNonRequiredSigner(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(aliceDID, acc(aliceAddr))
	f.ident.addActive(bobDID, acc(bobAddr))

	_, err := f.msg.CreateAgreement(f.ctx, &types.MsgCreateAgreement{
		Creator: acc(aliceAddr), Hash: []byte("agr-1"), RequiredSigners: []string{aliceDID},
	})
	require.NoError(t, err)

	_, err = f.msg.SignAgreement(f.ctx, &types.MsgSignAgreement{
		Signer: acc(bobAddr), Hash: []byte("agr-1"), SignerDid: bobDID, Signature: []byte("s"),
	})
	require.ErrorIs(t, err, types.ErrNotRequiredSigner)
}

func TestAgreement_RejectsDoubleSign(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(aliceDID, acc(aliceAddr))
	f.ident.addActive(bobDID, acc(bobAddr))

	_, err := f.msg.CreateAgreement(f.ctx, &types.MsgCreateAgreement{
		Creator: acc(aliceAddr), Hash: []byte("agr-1"), RequiredSigners: []string{aliceDID, bobDID},
	})
	require.NoError(t, err)
	_, err = f.msg.SignAgreement(f.ctx, &types.MsgSignAgreement{
		Signer: acc(aliceAddr), Hash: []byte("agr-1"), SignerDid: aliceDID, Signature: []byte("s"),
	})
	require.NoError(t, err)
	_, err = f.msg.SignAgreement(f.ctx, &types.MsgSignAgreement{
		Signer: acc(aliceAddr), Hash: []byte("agr-1"), SignerDid: aliceDID, Signature: []byte("s"),
	})
	require.ErrorIs(t, err, types.ErrAlreadySigned)
}

func TestAgreement_RejectsExpired(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(aliceDID, acc(aliceAddr))

	// deadline is in the past relative to the fixture block time (1_000_000)
	_, err := f.msg.CreateAgreement(f.ctx, &types.MsgCreateAgreement{
		Creator: acc(aliceAddr), Hash: []byte("agr-1"), RequiredSigners: []string{aliceDID}, Deadline: 100,
	})
	require.NoError(t, err)

	_, err = f.msg.SignAgreement(f.ctx, &types.MsgSignAgreement{
		Signer: acc(aliceAddr), Hash: []byte("agr-1"), SignerDid: aliceDID, Signature: []byte("s"),
	})
	require.ErrorIs(t, err, types.ErrAgreementExpired)
}

func TestAgreement_CompletedIsImmutable(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(aliceDID, acc(aliceAddr))

	_, err := f.msg.CreateAgreement(f.ctx, &types.MsgCreateAgreement{
		Creator: acc(aliceAddr), Hash: []byte("agr-1"), RequiredSigners: []string{aliceDID},
	})
	require.NoError(t, err)
	res, err := f.msg.SignAgreement(f.ctx, &types.MsgSignAgreement{
		Signer: acc(aliceAddr), Hash: []byte("agr-1"), SignerDid: aliceDID, Signature: []byte("s"),
	})
	require.NoError(t, err)
	require.True(t, res.Completed)

	// no further signing or cancelling on a completed agreement
	_, err = f.msg.SignAgreement(f.ctx, &types.MsgSignAgreement{
		Signer: acc(aliceAddr), Hash: []byte("agr-1"), SignerDid: aliceDID, Signature: []byte("s"),
	})
	require.ErrorIs(t, err, types.ErrAgreementClosed)
	_, err = f.msg.CancelAgreement(f.ctx, &types.MsgCancelAgreement{Creator: acc(aliceAddr), Hash: []byte("agr-1")})
	require.ErrorIs(t, err, types.ErrAgreementClosed)
}

func TestAgreement_CancelByCreatorOnly(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(aliceDID, acc(aliceAddr))

	_, err := f.msg.CreateAgreement(f.ctx, &types.MsgCreateAgreement{
		Creator: acc(aliceAddr), Hash: []byte("agr-1"), RequiredSigners: []string{aliceDID},
	})
	require.NoError(t, err)

	// non-creator cannot cancel
	_, err = f.msg.CancelAgreement(f.ctx, &types.MsgCancelAgreement{Creator: acc(bobAddr), Hash: []byte("agr-1")})
	require.ErrorIs(t, err, types.ErrUnauthorized)

	// creator cancels; afterwards it cannot be signed
	_, err = f.msg.CancelAgreement(f.ctx, &types.MsgCancelAgreement{Creator: acc(aliceAddr), Hash: []byte("agr-1")})
	require.NoError(t, err)
	_, err = f.msg.SignAgreement(f.ctx, &types.MsgSignAgreement{
		Signer: acc(aliceAddr), Hash: []byte("agr-1"), SignerDid: aliceDID, Signature: []byte("s"),
	})
	require.ErrorIs(t, err, types.ErrAgreementClosed)
}

func TestAgreement_BadSignatureRejected(t *testing.T) {
	f := setup(t, phicrypto.RejectAll())
	f.ident.addActive(aliceDID, acc(aliceAddr))

	_, err := f.msg.CreateAgreement(f.ctx, &types.MsgCreateAgreement{
		Creator: acc(aliceAddr), Hash: []byte("agr-1"), RequiredSigners: []string{aliceDID},
	})
	require.NoError(t, err)
	_, err = f.msg.SignAgreement(f.ctx, &types.MsgSignAgreement{
		Signer: acc(aliceAddr), Hash: []byte("agr-1"), SignerDid: aliceDID, Signature: []byte("bad"),
	})
	require.ErrorIs(t, err, types.ErrInvalidSignature)
}

// --- personal anchors ---

func TestAnchorPersonal(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(aliceDID, acc(aliceAddr))

	_, err := f.msg.AnchorPersonal(f.ctx, &types.MsgAnchorPersonal{
		Owner: acc(aliceAddr), OwnerDid: aliceDID, AnchorHash: []byte("doc-1"), Signature: []byte("s"),
	})
	require.NoError(t, err)
	require.True(t, f.k.HasPersonalAnchor(f.ctx, aliceDID, []byte("doc-1")))

	// duplicate anchor rejected
	_, err = f.msg.AnchorPersonal(f.ctx, &types.MsgAnchorPersonal{
		Owner: acc(aliceAddr), OwnerDid: aliceDID, AnchorHash: []byte("doc-1"), Signature: []byte("s"),
	})
	require.ErrorIs(t, err, types.ErrPersonalAnchorExists)
}

func TestAnchorPersonal_BadSignatureRejected(t *testing.T) {
	f := setup(t, phicrypto.RejectAll())
	f.ident.addActive(aliceDID, acc(aliceAddr))

	_, err := f.msg.AnchorPersonal(f.ctx, &types.MsgAnchorPersonal{
		Owner: acc(aliceAddr), OwnerDid: aliceDID, AnchorHash: []byte("doc-1"), Signature: []byte("bad"),
	})
	require.ErrorIs(t, err, types.ErrInvalidSignature)
}

func TestAnchorPersonal_OwnerMustControlDID(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(aliceDID, acc(aliceAddr))

	_, err := f.msg.AnchorPersonal(f.ctx, &types.MsgAnchorPersonal{
		Owner: acc(bobAddr), OwnerDid: aliceDID, AnchorHash: []byte("doc-1"), Signature: []byte("s"),
	})
	require.ErrorIs(t, err, types.ErrUnauthorized)
}

// --- params ---

func TestUpdateParams_OnlyAuthority(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())

	_, err := f.msg.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority: acc(aliceAddr), Params: types.Params{MaxAgreementSigners: 5},
	})
	require.Error(t, err)

	_, err = f.msg.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority: f.k.GetAuthority(), Params: types.Params{MaxAgreementSigners: 5},
	})
	require.NoError(t, err)
	require.Equal(t, uint32(5), f.k.GetParams(f.ctx).MaxAgreementSigners)
}

// --- genesis round-trip ---

func TestGenesis_RoundTrip(t *testing.T) {
	f := setup(t, phicrypto.AcceptAll())
	f.ident.addActive(issuerDID, acc(issuerAddr))
	f.ident.addActive(subjectDID, acc(subjectAddr))
	f.ident.addActive(aliceDID, acc(aliceAddr))

	f.registerTemplate(t, "phi.kyc.v1")
	_, err := f.msg.AnchorCredential(f.ctx, &types.MsgAnchorCredential{
		Issuer: acc(issuerAddr), IssuerDid: issuerDID, SubjectDid: subjectDID,
		TemplateId: "phi.kyc.v1", TemplateVersion: 1, CredentialHash: []byte("cred-1"), IssuerSig: []byte("sig"),
	})
	require.NoError(t, err)
	_, err = f.msg.CreateAgreement(f.ctx, &types.MsgCreateAgreement{
		Creator: acc(aliceAddr), Hash: []byte("agr-1"), RequiredSigners: []string{aliceDID},
	})
	require.NoError(t, err)
	_, err = f.msg.AnchorPersonal(f.ctx, &types.MsgAnchorPersonal{
		Owner: acc(aliceAddr), OwnerDid: aliceDID, AnchorHash: []byte("doc-1"), Signature: []byte("s"),
	})
	require.NoError(t, err)

	exported := f.k.ExportGenesis(f.ctx)
	require.NoError(t, exported.Validate())

	// re-import into a fresh keeper and re-export; the two must match
	f2 := setup(t, phicrypto.AcceptAll())
	f2.k.InitGenesis(f2.ctx, *exported)
	require.Equal(t, exported, f2.k.ExportGenesis(f2.ctx))
}
