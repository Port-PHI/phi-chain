// SPDX-License-Identifier: Apache-2.0

package types

import (
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/internal/storeentry"
)

// DefaultGenesis returns the default genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:       DefaultParams(),
		Institutions: []Institution{},
		RoleGrants:   []RoleGrant{},
		FxRequests:   []FxEntryRequest{},
	}
}

func (gs GenesisState) genesisTimestamps() []struct {
	label string
	value int64
} {
	out := []struct {
		label string
		value int64
	}{
		{"params.emergency_redemption.started_at", gs.Params.EmergencyRedemption.StartedAt},
	}
	for _, inst := range gs.Institutions {
		out = append(out, struct {
			label string
			value int64
		}{fmt.Sprintf("institution %s: last_attested_at", inst.Id), inst.LastAttestedAt})
	}
	return out
}

// ValidateAtTime is Validate plus time-dependent checks: a future timestamp would permanently disable the §4.6 staleness gate.
func (gs GenesisState) ValidateAtTime(blockTime int64) error {
	if err := gs.Validate(); err != nil {
		return err
	}
	for _, ts := range gs.genesisTimestamps() {
		if ts.value > blockTime {
			return fmt.Errorf("%s is %d, which is after the genesis block time %d", ts.label, ts.value, blockTime)
		}
	}
	return nil
}

// Validate checks the structural validity of the genesis state (the global solvency invariant is checked in keeper.InitGenesis).
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	// Negative timestamps are refused statelessly: a negative date reads as maximally stale.
	for _, ts := range gs.genesisTimestamps() {
		if ts.value < 0 {
			return fmt.Errorf("%s is negative (%d)", ts.label, ts.value)
		}
	}
	seen := make(map[string]bool)
	for _, inst := range gs.Institutions {
		if inst.Id == "" {
			return fmt.Errorf("institution with empty id")
		}
		if len(inst.Id) > MaxInstitutionIDLen {
			// Composite keys (RoleKey/DepositKey/...) use a single-byte length prefix; an id of length >= 256 would wrap it and create colliding key prefixes.
			return fmt.Errorf("institution %s: id length %d > %d", inst.Id, len(inst.Id), MaxInstitutionIDLen)
		}
		if seen[inst.Id] {
			return fmt.Errorf("duplicate institution id: %s", inst.Id)
		}
		seen[inst.Id] = true

		// The root admin must be a real key.
		if _, err := sdk.AccAddressFromBech32(inst.Admin); err != nil {
			return fmt.Errorf("institution %s: invalid admin address %q: %w", inst.Id, inst.Admin, err)
		}
		if inst.InstitutionType != INSTITUTION_TYPE_FINANCIAL && inst.InstitutionType != INSTITUTION_TYPE_FX {
			return fmt.Errorf("institution %s: invalid institution_type %d", inst.Id, inst.InstitutionType)
		}

		vb, ok := math.NewIntFromString(inst.VaultBalance)
		if !ok {
			return fmt.Errorf("institution %s: invalid vault_balance %q", inst.Id, inst.VaultBalance)
		}
		if vb.IsNegative() {
			return fmt.Errorf("institution %s: negative vault_balance", inst.Id)
		}
		if ar, ok := math.NewIntFromString(inst.AttestedReserve); !ok || ar.IsNegative() {
			return fmt.Errorf("institution %s: invalid attested_reserve %q", inst.Id, inst.AttestedReserve)
		}
		if err := inst.Params.Validate(); err != nil {
			return fmt.Errorf("institution %s params: %w", inst.Id, err)
		}
		// The redeem floor applies here too.
		if name, v, below := inst.Params.RedeemCapsBelowFloor(CapInt(gs.Params.RedeemFloorPerTx)); below {
			return fmt.Errorf("institution %s: %s=%s is below the protocol redeem floor %s",
				inst.Id, name, v, CapInt(gs.Params.RedeemFloorPerTx))
		}
	}
	// Role grants must reference an existing institution, a valid address, and a valid role, and must be unique per (institution, address) so an imported genesis cannot carry conflicting duplicate grants for the same sub-user.
	roleSeen := make(map[string]bool)
	for i, rg := range gs.RoleGrants {
		if !seen[rg.Institution] {
			return fmt.Errorf("role_grant[%d]: unknown institution %q", i, rg.Institution)
		}
		if _, err := sdk.AccAddressFromBech32(rg.Address); err != nil {
			return fmt.Errorf("role_grant[%d]: invalid address: %w", i, err)
		}
		if rg.Role <= INSTITUTION_ROLE_UNSPECIFIED || rg.Role > INSTITUTION_ROLE_VIEWER {
			return fmt.Errorf("role_grant[%d]: invalid role %d", i, rg.Role)
		}
		dedupKey := rg.Institution + "\x00" + rg.Address
		if roleSeen[dedupKey] {
			return fmt.Errorf("role_grant[%d]: duplicate grant for address %s in institution %q", i, rg.Address, rg.Institution)
		}
		roleSeen[dedupKey] = true
	}
	// Pending fx onboarding requests: each must have a unique fx_id that is not already an institution, a valid applicant address, a named guarantor, and a non-terminal status (REQUESTED/GUARANTEED).
	fxSeen := make(map[string]bool)
	for i, req := range gs.FxRequests {
		if req.FxId == "" {
			return fmt.Errorf("fx_request[%d]: empty fx_id", i)
		}
		if len(req.FxId) > MaxInstitutionIDLen {
			return fmt.Errorf("fx_request[%d]: fx_id length %d > %d", i, len(req.FxId), MaxInstitutionIDLen)
		}
		if fxSeen[req.FxId] {
			return fmt.Errorf("fx_request[%d]: duplicate fx_id %q", i, req.FxId)
		}
		fxSeen[req.FxId] = true
		if seen[req.FxId] {
			return fmt.Errorf("fx_request[%d]: fx_id %q is already a registered institution", i, req.FxId)
		}
		if _, err := sdk.AccAddressFromBech32(req.Applicant); err != nil {
			return fmt.Errorf("fx_request[%d]: invalid applicant: %w", i, err)
		}
		if req.GuarantorId == "" {
			return fmt.Errorf("fx_request[%d]: empty guarantor_id", i)
		}
		if req.Status != FxEntryStatus_FX_ENTRY_REQUESTED && req.Status != FxEntryStatus_FX_ENTRY_GUARANTEED {
			return fmt.Errorf("fx_request[%d]: non-pending status %s", i, req.Status)
		}
	}
	// Raw marker round-trip: each entry's key must carry the prefix of its own field (so an imported genesis cannot write under another prefix, e.g.
	if err := validateStoreEntries("deposit_markers", gs.DepositMarkers,
		Rule{Name: "deposit marker", Prefix: DepositPrefix, Value: validateDepositMarkerValue}); err != nil {
		return err
	}
	if err := validateStoreEntries("cap_counters", gs.CapCounters,
		Rule{Name: "cap counter", Prefix: CounterPrefix, Value: validateCapCounterValue}); err != nil {
		return err
	}
	if err := validateStoreEntries("approvals", gs.Approvals,
		Rule{Name: "approval epoch", Prefix: ApprovalPrefix, Value: validateApprovalValue}); err != nil {
		return err
	}
	// The residual prefixes share one field, so each key is confined to the residual keyspace as a whole rather than to a single prefix.
	return validateStoreEntries("store_entries", gs.StoreEntries, residualRules()...)
}

func residualRules() []Rule {
	return []Rule{
		// The one that matters most: read as a bare uint64, and a wrong width reads as epoch 0, which un-retires every approval a previous admin-set change invalidated.
		{Name: "admin-set epoch", Prefix: AdminEpochPrefix, Value: storeentry.FixedLen(8)},
		{Name: "holder KYC tier", Prefix: HolderKycTierPrefix, Value: storeentry.FixedLen(4)},
		// Read straight back as an account address; a malformed one is an attestor nobody can be, which silently blocks minting, or — worse if it collides — one the wrong party controls.
		{Name: "last attestor", Prefix: LastAttestorPrefix, Value: validateAttestorValue},
		{Name: "redeem-subject counter", Prefix: RedeemSubjectPrefix, Value: validateCapCounterValue},
	}
}

func validateAttestorValue(v []byte) error {
	if err := sdk.VerifyAddressFormat(v); err != nil {
		return fmt.Errorf("value is not an account address: %w", err)
	}
	return nil
}

// Rule is this module's alias for a shared store-entry rule, so the rule tables below read in the module's own vocabulary.
type Rule = storeentry.Rule

func validateStoreEntries(field string, entries []StoreEntry, rules ...Rule) error {
	kvs := make([]storeentry.KV, len(entries))
	for i, e := range entries {
		kvs[i] = storeentry.KV{Key: e.Key, Value: e.Value}
	}
	return storeentry.Validate(field, kvs, rules...)
}

func validateDepositMarkerValue(v []byte) error {
	if len(v) != 1 || v[0] != DepositMarkerByte {
		return fmt.Errorf("value is not the deposit marker sentinel")
	}
	return nil
}

func validateCapCounterValue(v []byte) error {
	amt, ok := math.NewIntFromString(string(v))
	if !ok {
		return fmt.Errorf("value is not a decimal integer counter")
	}
	if amt.IsNegative() {
		return fmt.Errorf("value is a negative counter")
	}
	return nil
}

func validateApprovalValue(v []byte) error {
	if len(v) != 8 {
		return fmt.Errorf("value is not an 8-byte approval epoch")
	}
	return nil
}
