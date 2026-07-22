// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"encoding/binary"
	"encoding/json"
	"testing"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	identitytypes "github.com/Port-PHI/phi-chain/x/identity/types"
	insttypes "github.com/Port-PHI/phi-chain/x/institutions/types"
)

func seedAddr(label string) sdk.AccAddress {
	b := make([]byte, 20)
	copy(b, label)
	return sdk.AccAddress(b)
}

func seedRichGenesis(t *testing.T, genesis map[string]json.RawMessage, gv genesisValidator) map[string]json.RawMessage {
	t.Helper()
	scratch := newTestApp(t)
	cdc := scratch.AppCodec()

	genesis[identitytypes.ModuleName] = cdc.MustMarshalJSON(richIdentityGenesis(t, gv))
	genesis[insttypes.ModuleName] = cdc.MustMarshalJSON(richInstitutionsGenesis(t, cdc, genesis))
	return genesis
}

const founderDID = "did:phi:roundtrip-founder"

func richIdentityGenesis(t *testing.T, gv genesisValidator) *identitytypes.GenesisState {
	t.Helper()

	doc := func(did, controller string, status identitytypes.DIDStatus, uniq string) identitytypes.DIDDocument {
		return identitytypes.DIDDocument{
			Did: did, Controller: controller, PubKey: []byte("pk-" + did),
			UniquenessHash: []byte(uniq), Status: status,
			CreatedAt: genesisChainTime.Unix() - 1_000_000,
		}
	}

	suspended := seedAddr("rt-suspended________")
	revoked := seedAddr("rt-revoked__________")
	guarded := seedAddr("rt-guarded__________")

	gs := identitytypes.DefaultGenesis()
	gs.Identities = []identitytypes.DIDDocument{
		doc(founderDID, gv.addr.String(), identitytypes.DID_STATUS_ACTIVE, "uniq-founder"),
		doc("did:phi:rt-suspended", suspended.String(), identitytypes.DID_STATUS_SUSPENDED, "uniq-suspended"),
		doc("did:phi:rt-revoked", revoked.String(), identitytypes.DID_STATUS_REVOKED, "uniq-revoked"),
		doc("did:phi:rt-guarded", guarded.String(), identitytypes.DID_STATUS_ACTIVE, "uniq-guarded"),
	}
	gs.IdentityCount = uint64(len(gs.Identities))

	gs.GuardianSets = []identitytypes.GuardianSet{
		{Did: "did:phi:rt-guarded", Commitments: commitments("g", 3), Threshold: 2},
		{Did: "did:phi:rt-suspended", Commitments: commitments("s", 2), Threshold: 2},
	}

	gs.StoreEntries = []identitytypes.StoreEntry{
		{Key: identitytypes.IssuerNonceKey("did:phi:issuer", []byte("burned-nonce")), Value: []byte{1}},
		{Key: identitytypes.DIDToValidatorKey(founderDID), Value: []byte(sdk.ValAddress(gv.addr).String())},
		{Key: identitytypes.ValidatorToDIDKey(sdk.ValAddress(gv.addr).String()), Value: []byte(founderDID)},
	}
	return gs
}

func commitments(label string, n int) [][]byte {
	out := make([][]byte, 0, n)
	for i := 0; i < n; i++ {
		c := make([]byte, 32)
		copy(c, label)
		c[31] = byte(i)
		out = append(out, c)
	}
	return out
}

func totalGenesisSupply(t *testing.T, cdc codec.Codec, genesis map[string]json.RawMessage) math.Int {
	t.Helper()
	var bank banktypes.GenesisState
	cdc.MustUnmarshalJSON(genesis[banktypes.ModuleName], &bank)

	total := math.ZeroInt()
	for _, b := range bank.Balances {
		total = total.Add(b.Coins.AmountOf(cointypes.Denom))
	}
	require.True(t, total.IsPositive(), "the base genesis must fund something, or the vaults prove nothing")
	return total
}

func richInstitutionsGenesis(t *testing.T, cdc codec.Codec, genesis map[string]json.RawMessage) *insttypes.GenesisState {
	t.Helper()

	gs := insttypes.DefaultGenesis()
	supply := totalGenesisSupply(t, cdc, genesis)
	backing := supply.MulRaw(int64(gs.Params.PhiToToman)).QuoRaw(int64(cointypes.UphiPerPhi))
	require.True(t, backing.MulRaw(int64(cointypes.UphiPerPhi)).Equal(supply.MulRaw(int64(gs.Params.PhiToToman))),
		"the seeded supply must divide exactly, or solvency fails for arithmetic reasons")

	firstVault := backing.QuoRaw(4)
	secondVault := backing.Sub(firstVault)

	adminA, adminB := seedAddr("rt-inst-a-admin_____"), seedAddr("rt-inst-a-admin2____")
	adminC, adminD := seedAddr("rt-inst-b-admin_____"), seedAddr("rt-inst-b-admin2____")
	holder := seedAddr("rt-holder___________")
	attestor := seedAddr("rt-attestor_________")

	inst := func(id, admin string, vault math.Int, kind insttypes.InstitutionType) insttypes.Institution {
		return insttypes.Institution{
			Id: id, License: "lic-" + id, Admin: admin,
			VaultAccount: "vault-" + id, VaultApi: "https://" + id + ".example",
			Bond: "0", Status: insttypes.INSTITUTION_STATUS_HEALTHY,
			VaultBalance: vault.String(), AttestedReserve: vault.String(),
			InstitutionType: kind, LastAttestedAt: genesisChainTime.Unix(),
			Params: insttypes.InstitutionParams{},
		}
	}

	gs.Institutions = []insttypes.Institution{
		inst("rt-bank", adminA.String(), firstVault, insttypes.INSTITUTION_TYPE_FINANCIAL),
		inst("rt-fx", adminC.String(), secondVault, insttypes.INSTITUTION_TYPE_FX),
	}

	gs.RoleGrants = []insttypes.RoleGrant{
		{Institution: "rt-bank", Address: adminB.String(), Role: insttypes.INSTITUTION_ROLE_ADMIN},
		{Institution: "rt-bank", Address: attestor.String(), Role: insttypes.INSTITUTION_ROLE_COMPLIANCE},
		{Institution: "rt-fx", Address: adminD.String(), Role: insttypes.INSTITUTION_ROLE_ADMIN},
	}

	gs.DepositMarkers = []insttypes.StoreEntry{
		{Key: insttypes.DepositKey("rt-bank", "in", "ref-1"), Value: []byte{insttypes.DepositMarkerByte}},
	}
	gs.CapCounters = []insttypes.StoreEntry{
		{Key: insttypes.CounterTotalKey("rt-bank", "mint", dayOfGenesis()), Value: []byte("12345")},
		{Key: insttypes.CounterUserKey("rt-bank", "redeem", dayOfGenesis(), holder), Value: []byte("67")},
	}
	gs.Approvals = []insttypes.StoreEntry{
		{Key: insttypes.ApprovalKey("rt-bank", []byte("content-hash-0001"), adminA), Value: epochBytes(3)},
	}

	gs.StoreEntries = []insttypes.StoreEntry{
		{Key: insttypes.AdminEpochKey("rt-bank"), Value: epochBytes(3)},
		{Key: insttypes.AdminEpochKey("rt-fx"), Value: epochBytes(1)},
		{Key: insttypes.HolderKycTierKey("rt-bank", holder), Value: tierBytes(2)},
		{Key: insttypes.LastAttestorKey("rt-bank"), Value: attestor.Bytes()},
		{Key: insttypes.LastAttestorKey("rt-fx"), Value: adminD.Bytes()},
		{Key: insttypes.RedeemSubjectCounterKey(dayOfGenesis(), 'd', "did:phi:rt-guarded"), Value: []byte("890")},
	}
	return gs
}

func dayOfGenesis() int64 { return genesisChainTime.Unix() / 86_400 }

func epochBytes(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

func tierBytes(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}
