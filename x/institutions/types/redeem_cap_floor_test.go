// SPDX-License-Identifier: Apache-2.0

package types_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

type capField struct{ owner, name string }

func (c capField) String() string { return c.owner + "." + c.name }

func namesACap(n string) bool {
	return strings.Contains(n, "Redeem") || strings.Contains(n, "Cap") || strings.Contains(n, "DailyLimit")
}

func discoverRedeemCapFields() []capField {
	var out []capField
	for _, root := range []struct {
		owner string
		typ   reflect.Type
	}{
		{"Caps", reflect.TypeOf(types.Caps{})},
		{"Params", reflect.TypeOf(types.Params{})},
		{"KycTierLimit", reflect.TypeOf(types.KycTierLimit{})},
	} {
		walkCapFields(root.owner, root.typ, namesACap, 0, &out)
	}
	return out
}

const maxCapWalkDepth = 16

func walkCapFields(owner string, t reflect.Type, isCap func(string) bool, depth int, out *[]capField) {
	if depth > maxCapWalkDepth {
		return
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		elem := f.Type
		for elem.Kind() == reflect.Pointer || elem.Kind() == reflect.Slice || elem.Kind() == reflect.Array {
			elem = elem.Elem()
		}
		if elem.Kind() == reflect.Struct {
			walkCapFields(owner+"."+f.Name, elem, isCap, depth+1, out)
			continue
		}
		if f.Type.Kind() == reflect.String && isCap(f.Name) {
			*out = append(*out, capField{owner: owner, name: f.Name})
		}
	}
}

type capProbe struct {
	reject func(t *testing.T, subFloor string) error
	exempt string
}

// floorToman is the protocol floor these probes are written against, and subFloorToman is one Toman below it — positive, so it is a real cap, and small enough to strand a holder.
const (
	floorToman    = types.DefaultRedeemFloorToman
	subFloorToman = "99999"
)

const subFloorUphi = "99999"

func capProbes() map[string]capProbe {
	institutionCap := func(set func(p *types.InstitutionParams, v string)) capProbe {
		return capProbe{reject: func(t *testing.T, subFloor string) error {
			t.Helper()
			p := types.InstitutionParams{}
			set(&p, subFloor)
			name, v, below := p.RedeemCapsBelowFloor(types.CapInt(floorToman))
			if !below {
				return nil
			}
			return fmt.Errorf("%s=%s is below the floor", name, v)
		}}
	}

	emergencyCap := func(set func(e *types.EmergencyRedemption, v string)) capProbe {
		return capProbe{reject: func(t *testing.T, subFloor string) error {
			t.Helper()
			p := types.DefaultParams()
			set(&p.EmergencyRedemption, subFloor)
			return p.Validate()
		}}
	}

	return map[string]capProbe{
		"Caps.RedeemDaily": institutionCap(func(p *types.InstitutionParams, v string) {
			p.Caps.RedeemDaily = v
		}),
		"Caps.RedeemPerTx": institutionCap(func(p *types.InstitutionParams, v string) {
			p.Caps.RedeemPerTx = v
		}),
		"Caps.RedeemPerUser": institutionCap(func(p *types.InstitutionParams, v string) {
			p.Caps.RedeemPerUser = v
		}),
		"KycTierLimit.DailyLimitToman": institutionCap(func(p *types.InstitutionParams, v string) {
			p.KycTierLimits = []types.KycTierLimit{{Tier: 1, DailyLimitToman: v}}
		}),

		"Params.RedeemDailyCapPerDidUphi": {reject: func(t *testing.T, subFloor string) error {
			t.Helper()
			p := types.DefaultParams()
			p.RedeemDailyCapPerDidUphi = subFloor
			return p.Validate()
		}},

		"Params.EmergencyRedemption.CapBeforeDay30": emergencyCap(func(e *types.EmergencyRedemption, v string) { e.CapBeforeDay30 = v }),
		"Params.EmergencyRedemption.CapFromDay30":   emergencyCap(func(e *types.EmergencyRedemption, v string) { e.CapFromDay30 = v }),
		"Params.EmergencyRedemption.CapFromDay60":   emergencyCap(func(e *types.EmergencyRedemption, v string) { e.CapFromDay60 = v }),

		"Params.RedeemFloorPerTx": {
			exempt: "this is the floor itself, not a cap measured against it. It has its own rule — " +
				"Validate refuses a zero or empty floor, because an inert floor protects nobody.",
		},
	}
}

// TestRedeemCapFloor_EveryDiscoveredCapIsFloored is the structural net: discovery is reflective, so the table below cannot fall behind the structs.
func TestRedeemCapFloor_EveryDiscoveredCapIsFloored(t *testing.T) {
	fields := discoverRedeemCapFields()
	require.NotEmpty(t, fields, "no redeem cap fields were discovered — has the naming changed? "+
		"this test must be able to see them")

	probes := capProbes()
	seen := map[string]bool{}

	for _, f := range fields {
		t.Run(f.String(), func(t *testing.T) {
			seen[f.String()] = true

			probe, ok := probes[f.String()]
			require.True(t, ok,
				"%s is a redeem cap and nothing here says whether the floor applies to it. Add a probe "+
					"proving a sub-floor value is refused, or an exemption saying why it is not a cap.", f)

			if probe.exempt != "" {
				return
			}

			subFloor := subFloorToman
			if strings.HasSuffix(f.name, "Uphi") {
				subFloor = subFloorUphi
			}
			require.Error(t, probe.reject(t, subFloor),
				"%s accepts %s, which is below the protocol floor — an institution or governance can "+
					"set it and strand holders", f, subFloor)
		})
	}

	for name := range probes {
		require.True(t, seen[name],
			"capProbes() names %q, which reflection does not find — the probe is stale", name)
	}
}

// A cap at or above the floor must still be accepted.
func TestRedeemCapFloor_CapsAtOrAboveTheFloorAreAccepted(t *testing.T) {
	p := types.DefaultParams()
	p.RedeemDailyCapPerDidUphi = "1000000"
	require.NoError(t, p.Validate())

	p.RedeemDailyCapPerDidUphi = ""
	require.NoError(t, p.Validate())
	p.RedeemDailyCapPerDidUphi = "0"
	require.NoError(t, p.Validate())

	require.NoError(t, types.DefaultParams().Validate())
	require.True(t, types.CapInt(types.DefaultRedeemDailyCapPerDidUphi).
		GTE(math.NewInt(1_000_000)), "the default per-DID cap must clear the floor")
}

type advDepth2 struct {
	RedeemDeep string // a cap two structs down
	Bystander  string // a plain field at depth 2 — must NOT be discovered
}

type advDepth1 struct {
	CapMiddle string // a cap one struct down, named with "Cap" not "Redeem"
	Nested    advDepth2
}

type advRoot struct {
	RedeemTop     string      // a cap at the root
	DailyLimitFoo string      // a differently-named cap (the tier-limit shape)
	Child         advDepth1   // recurse
	ChildPtr      *advDepth1  // pointer to struct: must still be walked
	Children      []advDepth2 // slice of struct: must still be walked
	Plain         string      // not a cap
	Count         uint64      // not a string, never a cap here
}

// TestRedeemCapFloor_WalkFindsCapsAtEveryDepth plants a cap at depths 0, 1 and 2, plus a differently- named cap and a struct reached only through a pointer and through a slice, and proves the walk discovers every one — and none of the non-cap fields.
func TestRedeemCapFloor_WalkFindsCapsAtEveryDepth(t *testing.T) {
	var out []capField
	walkCapFields("Root", reflect.TypeOf(advRoot{}), namesACap, 0, &out)

	found := map[string]bool{}
	for _, f := range out {
		found[f.String()] = true
	}

	require.True(t, found["Root.RedeemTop"], "a cap at the root")
	require.True(t, found["Root.DailyLimitFoo"], "a differently-named cap at the root")
	require.True(t, found["Root.Child.CapMiddle"], "a cap one struct down, named with Cap not Redeem")
	require.True(t, found["Root.Child.Nested.RedeemDeep"], "a cap two structs down")
	require.True(t, found["Root.ChildPtr.CapMiddle"], "a cap reached only through a pointer")
	require.True(t, found["Root.Children.RedeemDeep"], "a cap reached only through a slice")

	require.False(t, found["Root.Plain"], "a non-cap field must not be discovered")
	require.False(t, found["Root.Child.Nested.Bystander"], "a non-cap field at depth 2 must not be discovered")
	for _, f := range out {
		require.NotEqual(t, "Count", f.name, "a non-string field must never be a cap")
	}
}

// TestRedeemCapFloor_UnflooredNestedCapWouldFail is the point of the whole exercise: if a discovered nested cap had NO probe and NO exemption, the structural test fails.
func TestRedeemCapFloor_UnflooredNestedCapWouldFail(t *testing.T) {
	for _, set := range []func(e *types.EmergencyRedemption){
		func(e *types.EmergencyRedemption) { e.CapBeforeDay30 = subFloorToman },
		func(e *types.EmergencyRedemption) { e.CapFromDay30 = subFloorToman },
		func(e *types.EmergencyRedemption) { e.CapFromDay60 = subFloorToman },
	} {
		p := types.DefaultParams()
		set(&p.EmergencyRedemption)
		require.Error(t, p.Validate(),
			"a sub-floor emergency cap must be refused — an unfloored one would strand every holder")
	}

	require.NoError(t, types.DefaultParams().Validate())
	p := types.DefaultParams()
	p.EmergencyRedemption = types.EmergencyRedemption{Active: true, CapBeforeDay30: "0", CapFromDay30: "", CapFromDay60: ""}
	require.NoError(t, p.Validate(), "a halt (0) and stepped defaults ('') are not sub-floor caps")
}
