// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"reflect"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

func (f fixture) setCaps(caps types.Caps) error {
	_, err := f.msg.UpdateInstitutionParams(f.ctx, &types.MsgUpdateInstitutionParams{
		Signer: f.oper.String(), Institution: "bank-a", Params: types.InstitutionParams{Caps: caps},
	})
	return err
}

// Every redeem cap is floored, not just the per-transaction one.
func TestRedeemFloor_AllThreeCapsAreFloored(t *testing.T) {
	for _, tc := range []struct {
		name string
		caps types.Caps
	}{
		{"per transaction", types.Caps{RedeemPerTx: "1"}},
		{"daily", types.Caps{RedeemDaily: "1"}},
		{"per user", types.Caps{RedeemPerUser: "1"}},
		{"a compliant per-tx cap alongside a sub-floor daily cap", types.Caps{RedeemPerTx: "100000", RedeemDaily: "1"}},
		{"a compliant per-tx cap alongside a sub-floor per-user cap", types.Caps{RedeemPerTx: "100000", RedeemPerUser: "1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			f.registerAndAttest(t, "bank-a", 1_000_000)
			require.ErrorIs(t, f.setCaps(tc.caps), types.ErrLooserThanFloor,
				"a redeem cap below the protocol floor must be rejected")
		})
	}
}

// Caps at or above the floor are accepted, and so is no cap at all.
func TestRedeemFloor_CompliantCapsAreAccepted(t *testing.T) {
	for _, tc := range []struct {
		name string
		caps types.Caps
	}{
		{"exactly at the floor", types.Caps{RedeemPerTx: "100", RedeemDaily: "100", RedeemPerUser: "100"}},
		{"well above the floor", types.Caps{RedeemPerTx: "500000", RedeemDaily: "900000", RedeemPerUser: "700000"}},
		{"no caps at all", types.Caps{}},
		{"explicitly uncapped", types.Caps{RedeemPerTx: "0", RedeemDaily: "0", RedeemPerUser: "0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			f.registerAndAttest(t, "bank-a", 1_000_000)
			require.NoError(t, f.setCaps(tc.caps), "a cap at or above the floor must be accepted")
		})
	}
}

// The floor must be positive by default, or it protects nobody out of the box.
func TestRedeemFloor_DefaultIsPositive(t *testing.T) {
	p := types.DefaultParams()
	require.NoError(t, p.Validate())
	require.True(t, types.CapInt(p.RedeemFloorPerTx).IsPositive(),
		"the shipped floor must be positive; an empty floor is silently inert")
	require.Equal(t, types.DefaultRedeemFloorToman, p.RedeemFloorPerTx)
}

// Governance may move the floor but not switch it off: a zero or empty floor is rejected.
func TestRedeemFloor_ValidateRejectsANonPositiveFloor(t *testing.T) {
	for _, bad := range []string{"", "0"} {
		p := types.DefaultParams()
		p.RedeemFloorPerTx = bad
		require.Error(t, p.Validate(), "a %q floor must be rejected: it enforces nothing", bad)
	}

	p := types.DefaultParams()
	p.RedeemFloorPerTx = "250000"
	require.NoError(t, p.Validate(), "governance may raise the floor")
}

type redeemCapSetter struct {
	field string
	set   func(p *types.InstitutionParams, v string)
}

func redeemCapSetters() []redeemCapSetter {
	return []redeemCapSetter{
		{"RedeemPerTx", func(p *types.InstitutionParams, v string) { p.Caps.RedeemPerTx = v }},
		{"RedeemDaily", func(p *types.InstitutionParams, v string) { p.Caps.RedeemDaily = v }},
		{"RedeemPerUser", func(p *types.InstitutionParams, v string) { p.Caps.RedeemPerUser = v }},
		{"KycTierLimits", func(p *types.InstitutionParams, v string) {
			p.KycTierLimits = []types.KycTierLimit{
				{Tier: 1, DailyLimitToman: "150"},
				{Tier: 2, DailyLimitToman: v},
				{Tier: 3, DailyLimitToman: ""},
			}
		}},
	}
}

type capInstallPath struct {
	name    string
	install func(t *testing.T, p types.InstitutionParams) error
}

func capInstallPaths() []capInstallPath {
	return []capInstallPath{
		{
			name: "UpdateInstitutionParams",
			install: func(t *testing.T, p types.InstitutionParams) error {
				f := setup(t)
				f.registerAndAttest(t, "bank-a", 1_000_000)
				_, err := f.msg.UpdateInstitutionParams(f.ctx, &types.MsgUpdateInstitutionParams{
					Signer: f.oper.String(), Institution: "bank-a", Params: p,
				})
				return err
			},
		},
		{
			name: "genesis",
			install: func(t *testing.T, p types.InstitutionParams) error {
				gs := types.DefaultGenesis()
				gs.Params.PhiToToman = 100_000
				gs.Params.RedeemFloorPerTx = "100"
				gs.Institutions = []types.Institution{{
					Id:              "bank-a",
					Admin:           someInstAddr("floor-admin_________"),
					InstitutionType: types.INSTITUTION_TYPE_FINANCIAL,
					VaultBalance:    "0",
					AttestedReserve: "0",
					Params:          p,
				}}
				return gs.Validate()
			},
		},
	}
}

// Every redeem cap field × every install path: below the floor refused, at/above and unset accepted.
func TestRedeemFloor_AppliesToEveryCapOnEveryInstallPath(t *testing.T) {
	for _, path := range capInstallPaths() {
		t.Run(path.name, func(t *testing.T) {
			for _, setter := range redeemCapSetters() {
				t.Run(setter.field, func(t *testing.T) {
					for _, tc := range []struct {
						name    string
						value   string
						wantErr bool
					}{
						{"one Toman (the lockout)", "1", true},
						{"just below the floor", "99", true},
						{"exactly at the floor", "100", false},
						{"above the floor", "150", false},
						{"unset means no cap", "", false},
						{"explicitly uncapped", "0", false},
					} {
						t.Run(tc.name, func(t *testing.T) {
							var p types.InstitutionParams
							setter.set(&p, tc.value)
							err := path.install(t, p)
							if tc.wantErr {
								require.Error(t, err,
									"%s=%s must be refused on the %s path", setter.field, tc.value, path.name)
								return
							}
							require.NoError(t, err)
						})
					}
				})
			}
		})
	}
}

// Completeness guard: every reflectable redeem-cap field is driven by the setter table.
func TestRedeemFloor_CoversEveryRedeemCapField(t *testing.T) {
	covered := map[string]bool{}
	for _, s := range redeemCapSetters() {
		covered[s.field] = true
	}

	capsType := reflect.TypeOf(types.Caps{})
	for i := 0; i < capsType.NumField(); i++ {
		name := capsType.Field(i).Name
		if !strings.HasPrefix(name, "Redeem") {
			continue // mint caps are a different control
		}
		require.True(t, covered[name],
			"Caps.%s can cap a redemption but is not covered by the redeem floor", name)
	}

	if _, ok := reflect.TypeOf(types.InstitutionParams{}).FieldByName("KycTierLimits"); ok {
		require.True(t, covered["KycTierLimits"], "KycTierLimits caps a redemption and must be floored")
	}
}

// A one-Toman tier limit cannot be installed; at the floor itself a holder can still redeem.
func TestRedeemFloor_TierLimitCannotStrandAHolder(t *testing.T) {
	holder := sdk.AccAddress([]byte("holder______________"))
	f := setupDIDCap(t, "200000000", map[string]string{holder.String(): "did:phi:holder"})
	f.registerAndAttest(t, "bank-a", 1_000_000)
	f.mintTo(t, "bank-a", holder, "10000", "dep-1")

	setTier := func(limit string) error {
		_, err := f.msg.UpdateInstitutionParams(f.ctx, &types.MsgUpdateInstitutionParams{
			Signer: f.oper.String(), Institution: "bank-a",
			Params: types.InstitutionParams{KycTierLimits: []types.KycTierLimit{{Tier: 1, DailyLimitToman: limit}}},
		})
		return err
	}

	require.ErrorIs(t, setTier("1"), types.ErrLooserThanFloor,
		"a one-Toman tier limit is a closure freeze and must be refused")
	require.NoError(t, setTier("100"), "the floor itself is the strictest an institution may go")

	require.NoError(t, f.redeem("bank-a", holder, "100", "red-tier"),
		"a holder must still be able to redeem the floor amount under the strictest legal tier limit")
}

// A holder cannot be stranded by an institution shrinking its caps to nothing.
func TestRedeemFloor_HolderIsNotStrandedByASubFloorCap(t *testing.T) {
	holder := sdk.AccAddress([]byte("holder______________"))
	f := setupDIDCap(t, "200000000", map[string]string{holder.String(): "did:phi:holder"})
	f.registerAndAttest(t, "bank-a", 1_000_000)
	f.mintTo(t, "bank-a", holder, "10000", "dep-1")

	require.ErrorIs(t, f.setCaps(types.Caps{RedeemDaily: "1"}), types.ErrLooserThanFloor)

	require.NoError(t, f.redeem("bank-a", holder, "5000", "red-1"),
		"a holder must not be strandable by an institution shrinking its own caps")
}
