// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"bytes"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

// instB is instA plus a trailing NUL: the two collide under the old raw-0x00 layout.
const (
	kycInstA = "bank"
	kycInstB = "bank\x00"
)

var kycHolderA = sdk.AccAddress(append([]byte{0x00}, []byte("kyc-iso-holder-A___")...)) // 20 bytes

func oldKycTierKey(instID string, holder sdk.AccAddress) []byte {
	k := append(append([]byte{}, types.HolderKycTierPrefix...), []byte(instID)...)
	k = append(k, 0x00)
	return append(k, holder.Bytes()...)
}

func oldKycTierPrefixFor(instID string) []byte {
	return append(append(append([]byte{}, types.HolderKycTierPrefix...), []byte(instID)...), 0x00)
}

// Under the length-prefixed scheme neither institution's KYC range contains the other's key.
func TestKycTierKey_KeyspacesAreDisjointAcrossInstitutions(t *testing.T) {
	require.True(t, bytes.HasPrefix(oldKycTierKey(kycInstA, kycHolderA), oldKycTierPrefixFor(kycInstB)),
		"precondition: under the old raw-0x00 layout, A's KYC key falls inside B's range")
	require.True(t, bytes.HasPrefix(oldKycTierKey(kycInstB, kycHolderA), oldKycTierPrefixFor(kycInstA)),
		"precondition: the overlap is bidirectional under the old layout")

	require.False(t, bytes.HasPrefix(types.HolderKycTierKey(kycInstA, kycHolderA), types.HolderKycTierPrefixFor(kycInstB)),
		"institution A's KYC key must not fall inside institution B's range")
	require.False(t, bytes.HasPrefix(types.HolderKycTierKey(kycInstB, kycHolderA), types.HolderKycTierPrefixFor(kycInstA)),
		"institution B's KYC key must not fall inside institution A's range")

	require.True(t, bytes.HasPrefix(types.HolderKycTierKey(kycInstA, kycHolderA), types.HolderKycTierPrefixFor(kycInstA)),
		"an institution's KYC key must still lie under its own range")
}

// Removing institution B (whose id collides with A's under the old layout) must leave A's KYC tier intact.
func TestKycTier_RemovingOneInstitutionKeepsAnothersTiers(t *testing.T) {
	f := setup(t)

	inst := func(id string) types.Institution {
		return types.Institution{
			Id: id, Admin: sdk.AccAddress([]byte("kyc-iso-root-admin__")).String(),
			InstitutionType: types.INSTITUTION_TYPE_FINANCIAL, VaultBalance: "0", AttestedReserve: "0",
		}
	}
	f.k.SetInstitution(f.ctx, inst(kycInstA))
	f.k.SetInstitution(f.ctx, inst(kycInstB))

	holderB := sdk.AccAddress([]byte("kyc-iso-holder-B____"))
	f.k.SetHolderKycTier(f.ctx, kycInstA, kycHolderA, 3)
	f.k.SetHolderKycTier(f.ctx, kycInstB, holderB, 2)

	_, hasA := f.k.HolderKycTier(f.ctx, kycInstA, kycHolderA)
	require.True(t, hasA)
	_, hasB := f.k.HolderKycTier(f.ctx, kycInstB, holderB)
	require.True(t, hasB)

	f.removeAndDrain(t, kycInstB)

	tierA, survivedA := f.k.HolderKycTier(f.ctx, kycInstA, kycHolderA)
	require.True(t, survivedA, "removing institution B must not purge institution A's KYC tier")
	require.Equal(t, uint32(3), tierA, "and it must be unchanged")

	_, survivedB := f.k.HolderKycTier(f.ctx, kycInstB, holderB)
	require.False(t, survivedB, "the removed institution's own KYC tiers must be gone")
}

// A KYC-tier record keyed under a NUL-bearing institution id survives export→import byte for byte.
func TestKycTier_GenesisRoundTripWithNulBearingID(t *testing.T) {
	f := setup(t)
	f.k.SetInstitution(f.ctx, types.Institution{
		Id: kycInstB, Admin: sdk.AccAddress([]byte("kyc-iso-gen-admin___")).String(),
		InstitutionType: types.INSTITUTION_TYPE_FINANCIAL, VaultBalance: "0", AttestedReserve: "0",
	})
	f.k.SetHolderKycTier(f.ctx, kycInstB, kycHolderA, 4)

	key := types.HolderKycTierKey(kycInstB, kycHolderA)
	before := append([]byte(nil), f.ctx.KVStore(f.key).Get(key)...)
	require.Len(t, before, 4, "precondition: the tier record is present")

	exported := f.k.ExportGenesis(f.ctx)
	require.NoError(t, exported.Validate(), "the exported genesis must validate")

	f2 := setup(t)
	f2.bank.supply[cointypes.Denom] = f.bank.GetSupply(f.ctx, cointypes.Denom).Amount
	require.NotPanics(t, func() { f2.k.InitGenesis(f2.ctx, *exported) })

	require.Equal(t, before, f2.ctx.KVStore(f2.key).Get(key),
		"the KYC-tier record must round-trip byte for byte under its length-prefixed key")
	tier, ok := f2.k.HolderKycTier(f2.ctx, kycInstB, kycHolderA)
	require.True(t, ok, "the imported KYC tier must be readable")
	require.Equal(t, uint32(4), tier)
}
