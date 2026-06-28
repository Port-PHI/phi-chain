// SPDX-License-Identifier: Apache-2.0

package types

import (
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
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

// Validate checks the structural validity of the genesis state (the global solvency invariant is checked in keeper.InitGenesis).
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, inst := range gs.Institutions {
		if inst.Id == "" {
			return fmt.Errorf("institution with empty id")
		}
		if len(inst.Id) > MaxInstitutionIDLen {
			// Composite keys (RoleKey/DepositKey/...) use a single-byte length prefix; an id
			// of length >= 256 would wrap it and create colliding key prefixes.
			return fmt.Errorf("institution %s: id length %d > %d", inst.Id, len(inst.Id), MaxInstitutionIDLen)
		}
		if seen[inst.Id] {
			return fmt.Errorf("duplicate institution id: %s", inst.Id)
		}
		seen[inst.Id] = true

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
	}
	// Role grants must reference an existing institution, a valid address, and a valid role, and must
	// be unique per (institution, address) so an imported genesis cannot carry conflicting duplicate
	// grants for the same sub-user.
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
	// Pending fx onboarding requests: each must have a unique fx_id that is not already an institution,
	// a valid applicant address, a named guarantor, and a non-terminal status (REQUESTED/GUARANTEED).
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
	// Raw marker round-trip: each entry's key must carry the prefix of its own field, so an
	// imported genesis cannot write under another prefix (e.g. overwrite an Institution or Params).
	if err := validateStoreEntries("deposit_markers", gs.DepositMarkers, DepositPrefix); err != nil {
		return err
	}
	if err := validateStoreEntries("cap_counters", gs.CapCounters, CounterPrefix); err != nil {
		return err
	}
	if err := validateStoreEntries("approvals", gs.Approvals, ApprovalPrefix); err != nil {
		return err
	}
	return nil
}

// validateStoreEntries checks that every raw marker key begins with the expected single-byte prefix
// (the marker prefixes are distinct single bytes), confining an imported genesis to its own keyspace.
func validateStoreEntries(field string, entries []StoreEntry, prefix []byte) error {
	for i, e := range entries {
		if len(e.Key) < len(prefix) || string(e.Key[:len(prefix)]) != string(prefix) {
			return fmt.Errorf("%s[%d]: key is not under the expected prefix", field, i)
		}
	}
	return nil
}
