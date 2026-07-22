// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"

	governancekeeper "github.com/Port-PHI/phi-chain/x/governance/keeper"
	governancetypes "github.com/Port-PHI/phi-chain/x/governance/types"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	credentialstypes "github.com/Port-PHI/phi-chain/x/credentials/types"
	identitytypes "github.com/Port-PHI/phi-chain/x/identity/types"
	insttypes "github.com/Port-PHI/phi-chain/x/institutions/types"
	votingtypes "github.com/Port-PHI/phi-chain/x/voting/types"
)

func seedEveryKeyspace(t *testing.T, c *roundTripChain, gv genesisValidator) {
	t.Helper()
	ctx := c.writeCtx()

	seedNonDefaultParams(t, c, ctx)
	seedIdentityKeyspaces(t, c, ctx, gv)
	seedInstitutionsKeyspaces(t, c, ctx)
	seedCoinKeyspaces(c, ctx)
	seedCredentialsKeyspaces(c, ctx)
	seedVotingKeyspaces(c, ctx)
	seedGovernanceKeyspaces(t, c, ctx)

	c.commit()
}

func seedNonDefaultParams(t *testing.T, c *roundTripChain, ctx sdk.Context) {
	t.Helper()

	coinP := c.app.CoinKeeper.GetParams(ctx)
	coinP.CoinAgeThresholdSeconds++
	require.NoError(t, c.app.CoinKeeper.SetParams(ctx, coinP))

	credP := c.app.CredentialsKeeper.GetParams(ctx)
	credP.MaxAgreementSigners++
	require.NoError(t, c.app.CredentialsKeeper.SetParams(ctx, credP))

	discP := c.app.DisclosureKeeper.GetParams(ctx)
	discP.MaxProofSizeBytes++
	require.NoError(t, c.app.DisclosureKeeper.SetParams(ctx, discP))

	govP := c.app.GovernanceKeeper.GetParams(ctx)
	govP.VoteRoutes = append(govP.VoteRoutes, governancetypes.VoteRouteEntry{
		MsgTypeUrl: "/phi.roundtrip.nondefault.Probe", Route: governancetypes.VOTE_ROUTE_PUBLIC,
	})
	require.NoError(t, c.app.GovernanceKeeper.SetParams(ctx, govP))

	idP := c.app.IdentityKeeper.GetParams(ctx)
	idP.RecoveryRequestTtlSeconds++
	require.NoError(t, c.app.IdentityKeeper.SetParams(ctx, idP))

	votP := c.app.VotingKeeper.GetParams(ctx)
	votP.MaxProofSizeBytes++
	require.NoError(t, c.app.VotingKeeper.SetParams(ctx, votP))

	instP := c.app.InstitutionsKeeper.GetParams(ctx)
	instP.ProtocolFeeBps += 5
	instP.RedeemDailyCapPerDidUphi = nonDefaultPerDidCapUphi
	require.NoError(t, c.app.InstitutionsKeeper.SetParams(ctx, instP))
}

const nonDefaultPerDidCapUphi = "300000000"

const (
	seededGuardedDID = "did:phi:rt-setter-guarded"
	seededIssuerDID  = "did:phi:rt-setter-issuer"
)

func seedIdentityKeyspaces(t *testing.T, c *roundTripChain, ctx sdk.Context, gv genesisValidator) {
	t.Helper()
	k := c.app.IdentityKeeper
	store := ctx.KVStore(c.app.GetKey(identitytypes.StoreKey))

	guardedCtrl := seedAddr("rt-setter-guarded___")
	k.SetIdentity(ctx, identitytypes.DIDDocument{
		Did: seededGuardedDID, Controller: guardedCtrl.String(), PubKey: []byte("pk-setter-guarded"),
		UniquenessHash: []byte("uniq-setter-guarded"), Status: identitytypes.DID_STATUS_ACTIVE,
		CreatedAt: genesisChainTime.Unix() - 2_000_000,
	})
	store.Set(identitytypes.UniquenessKey([]byte("uniq-setter-guarded")), []byte(seededGuardedDID))
	k.SetIdentityCount(ctx, k.GetIdentityCount(ctx)+1)
	k.SetTrustedIssuer(ctx, identitytypes.TrustedIssuer{
		Did: seededIssuerDID, PubKey: []byte("issuer-pk"), Active: true,
	})
	store.Set(identitytypes.IssuerNonceKey(seededIssuerDID, []byte("setter-burned-nonce")), []byte{1})
	k.BindValidatorToDID(ctx, seededGuardedDID, sdk.ValAddress(guardedCtrl).String())
	k.SetGuardianSet(ctx, identitytypes.GuardianSet{
		Did: seededGuardedDID, Commitments: commitments("setter", 3), Threshold: 2,
	})
	store.Set(identitytypes.GuardianEpochKey(seededGuardedDID), epochBytes(4))

	nonce := []byte("setter-recovery-nonce")
	newKey := []byte("setter-proposed-key")
	recoveryID := identitytypes.DeriveRecoveryID(seededGuardedDID, newKey, nonce)
	k.SetRecoveryRequest(ctx, identitytypes.RecoveryRequest{
		RecoveryId: recoveryID, Did: seededGuardedDID,
		ProposedNewPubKey: newKey, ProposedNewController: seedAddr("rt-setter-newdevice_").String(),
		KeyType: identitytypes.KEY_TYPE_SECP256R1, Method: identitytypes.RECOVERY_METHOD_SOCIAL,
		Status: identitytypes.RECOVERY_STATUS_PENDING, Nonce: nonce,
		InitiatedAt:  genesisChainTime.Unix(),
		ExecuteAfter: genesisChainTime.Unix() + 72*3600,
		ExpiresAt:    genesisChainTime.Unix() + 96*3600,
		DepositUphi:  "1000000", FeeUphi: "1000",
	})
	store.Set(identitytypes.RecoveryTallyEpochKey(recoveryID), epochBytes(4))
	store.Set(identitytypes.RecoveryNonceKey(seededGuardedDID, nonce), []byte{1})

	require.NotEmpty(t, recoveryID)
}

func seedInstitutionsKeyspaces(t *testing.T, c *roundTripChain, ctx sdk.Context) {
	t.Helper()
	k := c.app.InstitutionsKeeper
	store := ctx.KVStore(c.app.GetKey(insttypes.StoreKey))

	admin := seedAddr("rt-setter-inst-admin")
	second := seedAddr("rt-setter-inst-adm2_")
	holder := seedAddr("rt-setter-holder____")
	attestor := seedAddr("rt-setter-attestor__")

	k.SetInstitution(ctx, insttypes.Institution{
		Id: "rt-setter-bank", License: "lic-setter", Admin: admin.String(),
		VaultAccount: "vault-setter", VaultApi: "https://setter.example",
		Bond: "0", Status: insttypes.INSTITUTION_STATUS_HEALTHY,
		VaultBalance: "0", AttestedReserve: "0",
		InstitutionType: insttypes.INSTITUTION_TYPE_FINANCIAL,
		LastAttestedAt:  genesisChainTime.Unix(),
	})
	k.SetRole(ctx, "rt-setter-bank", second, insttypes.INSTITUTION_ROLE_ADMIN)
	k.SetHolderKycTier(ctx, "rt-setter-bank", holder, 3)
	k.SetFxRequest(ctx, insttypes.FxEntryRequest{
		FxId: "rt-setter-fx", Applicant: holder.String(), GuarantorId: "rt-setter-bank",
		Status: insttypes.FxEntryStatus_FX_ENTRY_REQUESTED,
	})
	store.Set(insttypes.DepositKey("rt-setter-bank", "in", "setter-ref"), []byte{insttypes.DepositMarkerByte})
	store.Set(insttypes.CounterTotalKey("rt-setter-bank", "mint", dayOfGenesis()), []byte("777"))
	store.Set(insttypes.ApprovalKey("rt-setter-bank", []byte("setter-content-hash"), admin), epochBytes(5))
	store.Set(insttypes.AdminEpochKey("rt-setter-bank"), epochBytes(5))
	store.Set(insttypes.LastAttestorKey("rt-setter-bank"), attestor.Bytes())
	store.Set(insttypes.RedeemSubjectCounterKey(dayOfGenesis(), 'd', seededGuardedDID), []byte("321"))
	store.Set(insttypes.CounterPruneCursorKey(), []byte("setter-cursor"))

	_, found := k.GetInstitution(ctx, "rt-setter-bank")
	require.True(t, found, "the seeded institution must be readable through the keeper")
}

func seedCoinKeyspaces(c *roundTripChain, ctx sdk.Context) {
	k := c.app.CoinKeeper
	owner := seedAddr("rt-setter-coinowner_").String()

	k.SetCoinAge(ctx, cointypes.CoinAge{
		Address: owner,
		Lots: []cointypes.CoinLot{
			{Amount: "1000000", AcquiredAt: genesisChainTime.Unix() - 86400},
			{Amount: "2500000", AcquiredAt: genesisChainTime.Unix()},
		},
	})
	k.IncrMicroUsed(ctx, genesisChainTime.Unix()/86400, owner)
}

func seedCredentialsKeyspaces(c *roundTripChain, ctx sdk.Context) {
	k := c.app.CredentialsKeeper

	k.SetTemplate(ctx, credentialstypes.CredentialTemplate{
		Id: "rt.setter.v1", Version: 1, OwnerDid: seededIssuerDID,
		SchemaHash: []byte("schema-setter"), Name: "RT",
		IssuerBbsPubkey: []byte("issuer-bbs-pubkey"),
		MessageCount:    4, DisclosableIndices: []uint32{1, 2},
		DisclosurePolicyHash: credentialstypes.DisclosurePolicyHash("rt.setter.v1", 1, 4, []uint32{1, 2}),
		Status:               credentialstypes.TEMPLATE_STATUS_ACTIVE,
	})
	k.SetAnchor(ctx, credentialstypes.CredentialAnchor{
		CredentialHash: []byte("rt-setter-credential"),
		TemplateId:     "rt.setter.v1", IssuerDid: seededIssuerDID, SubjectDid: seededGuardedDID,
		IssuedAt: genesisChainTime.Unix(),
	})
	k.SetAgreement(ctx, credentialstypes.Agreement{
		Hash: []byte("rt-setter-agreement"), Creator: seedAddr("rt-setter-agreementc").String(),
		RequiredSigners: []string{seededGuardedDID}, Status: credentialstypes.AGREEMENT_STATUS_PENDING,
		CreatedAt: genesisChainTime.Unix(),
	})
	k.SetPersonalAnchor(ctx, credentialstypes.PersonalAnchor{
		OwnerDid: seededGuardedDID, AnchorHash: []byte("rt-setter-personal"),
		AnchoredAt: genesisChainTime.Unix(),
	})
}

func seedVotingKeyspaces(c *roundTripChain, ctx sdk.Context) {
	k := c.app.VotingKeeper

	k.SetElection(ctx, votingtypes.Election{
		Id: "rt-setter-election", Title: "Q?", Options: []string{"yes", "no"},
		RequiredTemplateId: "rt.setter.v1", Creator: seedAddr("rt-setter-elcreator_").String(),
		VotingStart: 0, VotingEnd: genesisChainTime.Unix() + 86400,
		Status:        votingtypes.ELECTION_STATUS_OPEN,
		OptionTallies: []uint64{1, 0}, TotalVotes: 1,
	})
	k.SetBallot(ctx, votingtypes.Ballot{
		ElectionId: "rt-setter-election", Nullifier: []byte("rt-setter-nullifier1"),
		OptionIndex: 0, CastAt: genesisChainTime.Unix(),
	})
}

const setterProposalID = uint64(7)

var setterVoters = []struct {
	addr   sdk.AccAddress
	option v1.VoteOption
}{
	{seedAddr("rt-voter-yes-1______"), v1.OptionYes},
	{seedAddr("rt-voter-yes-2______"), v1.OptionYes},
	{seedAddr("rt-voter-no_________"), v1.OptionNo},
	{seedAddr("rt-voter-abstain____"), v1.OptionAbstain},
}

func setterBasis() governancekeeper.FrozenEligibility {
	return governancekeeper.FrozenEligibility{
		Denominator: 9,
		Cutoff:      genesisChainTime.Unix() - 500_000,
		FrozenAt:    genesisChainTime.Unix() - 1_000,
	}
}

func seedGovernanceKeyspaces(t *testing.T, c *roundTripChain, ctx sdk.Context) {
	t.Helper()
	k := c.app.GovernanceKeeper

	k.SetProposalEligibility(ctx, setterProposalID, setterBasis())
	for _, v := range setterVoters {
		require.True(t, k.RecordVote(ctx, setterProposalID, v.addr.Bytes(), int32(v.option), true))
	}
	require.False(t, k.RecordVote(ctx, setterProposalID,
		seedAddr("rt-voter-refused____").Bytes(), int32(v1.OptionYes), false))
	k.EnqueueForPruning(ctx, setterProposalID+1)
}
