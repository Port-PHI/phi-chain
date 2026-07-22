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
		Params:           DefaultParams(),
		Identities:       []DIDDocument{},
		IdentityCount:    0,
		TrustedIssuers:   []TrustedIssuer{},
		GuardianSets:     []GuardianSet{},
		RecoveryRequests: []RecoveryRequest{},
	}
}

// Validate checks the genesis state for correctness.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	seenDID := make(map[string]bool)
	seenUniq := make(map[string]bool)
	for _, d := range gs.Identities {
		if err := ValidateDID(d.Did); err != nil {
			return fmt.Errorf("genesis identity: %w", err)
		}
		if seenDID[d.Did] {
			return fmt.Errorf("duplicate DID in genesis: %s", d.Did)
		}
		seenDID[d.Did] = true

		// Mirror the runtime's immutable identity invariants.
		if len(d.PubKey) == 0 || len(d.PubKey) > MaxPubKeyLen {
			return fmt.Errorf("identity %s: pub_key length %d (must be 1..%d)", d.Did, len(d.PubKey), MaxPubKeyLen)
		}
		if _, err := sdk.AccAddressFromBech32(d.Controller); err != nil {
			return fmt.Errorf("identity %s: invalid controller address %q: %w", d.Did, d.Controller, err)
		}
		if d.Status != DID_STATUS_ACTIVE && d.Status != DID_STATUS_SUSPENDED && d.Status != DID_STATUS_REVOKED {
			return fmt.Errorf("identity %s: unknown status %d (must be ACTIVE, SUSPENDED, or REVOKED)", d.Did, d.Status)
		}
		// The curve must be one the chain can verify against (UNSPECIFIED is legal and means r1).
		if _, err := CurveForKeyType(d.KeyType); err != nil {
			return fmt.Errorf("identity %s: %w", d.Did, err)
		}

		// An empty uniqueness marker would silently bypass the one-human-one-DID guarantee.
		if len(d.UniquenessHash) == 0 {
			return fmt.Errorf("identity %s: empty uniqueness marker", d.Did)
		}
		if len(d.UniquenessHash) > MaxUniquenessHashLen {
			return fmt.Errorf("identity %s: uniqueness marker length %d exceeds %d", d.Did, len(d.UniquenessHash), MaxUniquenessHashLen)
		}
		uniq := string(d.UniquenessHash)
		if seenUniq[uniq] {
			return fmt.Errorf("duplicate uniqueness marker in genesis for DID: %s", d.Did)
		}
		seenUniq[uniq] = true
	}
	if uint64(len(gs.Identities)) > gs.IdentityCount {
		return fmt.Errorf("identity_count (%d) is less than number of identities (%d)", gs.IdentityCount, len(gs.Identities))
	}
	// Trusted issuers: unique, valid DID, non-empty public key.
	seenIssuer := make(map[string]bool)
	for i, ti := range gs.TrustedIssuers {
		if err := ValidateDID(ti.Did); err != nil {
			return fmt.Errorf("trusted_issuer[%d]: %w", i, err)
		}
		if seenIssuer[ti.Did] {
			return fmt.Errorf("duplicate trusted issuer: %s", ti.Did)
		}
		seenIssuer[ti.Did] = true
		if len(ti.PubKey) == 0 || len(ti.PubKey) > MaxPubKeyLen {
			return fmt.Errorf("trusted_issuer %s: pub_key length %d (must be 1..%d)", ti.Did, len(ti.PubKey), MaxPubKeyLen)
		}
	}
	// Raw marker round-trip: each StoreEntry must lie under a known marker prefix AND its value must decode under that prefix's encoding (values are read fail-soft, so a malformed one silently changes a rule — e.g.
	kvs := make([]storeentry.KV, len(gs.StoreEntries))
	for i, e := range gs.StoreEntries {
		kvs[i] = storeentry.KV{Key: e.Key, Value: e.Value}
	}
	if err := storeentry.Validate("store_entries", kvs, storeEntryRules()...); err != nil {
		return err
	}

	status := make(map[string]DIDStatus, len(gs.Identities))
	for _, d := range gs.Identities {
		status[d.Did] = d.Status
	}

	// notTerminated reports whether a DID may still hold state outliving a freeze (guardian set, open recovery, validator binding): ACTIVE and SUSPENDED may (suspension is reversible), REVOKED may not.
	notTerminated := func(did string) bool {
		st, ok := status[did]
		return ok && (st == DID_STATUS_ACTIVE || st == DID_STATUS_SUSPENDED)
	}

	// Validator↔DID binding: the did→valoper and valoper→did entries must be a bijection, and every bound DID must exist and not be terminated (one validator ↔ one live human).
	didToVal := make(map[string]string)
	valToDID := make(map[string]string)
	for i, e := range gs.StoreEntries {
		switch {
		case len(e.Key) > len(DIDToValidatorPrefix) && string(e.Key[:len(DIDToValidatorPrefix)]) == string(DIDToValidatorPrefix):
			did := string(e.Key[len(DIDToValidatorPrefix):])
			if _, dup := didToVal[did]; dup {
				return fmt.Errorf("store_entry[%d]: duplicate did→validator binding for %s", i, did)
			}
			didToVal[did] = string(e.Value)
		case len(e.Key) > len(ValidatorToDIDPrefix) && string(e.Key[:len(ValidatorToDIDPrefix)]) == string(ValidatorToDIDPrefix):
			valoper := string(e.Key[len(ValidatorToDIDPrefix):])
			if _, dup := valToDID[valoper]; dup {
				return fmt.Errorf("store_entry[%d]: duplicate validator→did binding for %s", i, valoper)
			}
			valToDID[valoper] = string(e.Value)
		}
	}
	for did, valoper := range didToVal {
		if valToDID[valoper] != did {
			return fmt.Errorf("validator binding not bijective: did %s → %s, but %s → %q", did, valoper, valoper, valToDID[valoper])
		}
		if !notTerminated(did) {
			return fmt.Errorf("validator-bound did %s must exist and not be revoked", did)
		}
	}
	for valoper, did := range valToDID {
		if didToVal[did] != valoper {
			return fmt.Errorf("validator binding not bijective: %s → %s, but did %s → %q", valoper, did, did, didToVal[did])
		}
	}

	// Guardian commitment sets: the same bounds the keeper enforces at runtime, re-checked here so a curated or compromised genesis cannot seed an over-sized or unusable recovery configuration — one set per DID, within the max_guardians cap, protecting an ACTIVE identity present in this genesis.
	seenGuardianDID := make(map[string]bool, len(gs.GuardianSets))
	for i, g := range gs.GuardianSets {
		if err := ValidateGuardianSetBasic(g.Did, g.Commitments, g.Threshold); err != nil {
			return fmt.Errorf("guardian_sets[%d]: %w", i, err)
		}
		if seenGuardianDID[g.Did] {
			return fmt.Errorf("duplicate guardian set for DID: %s", g.Did)
		}
		seenGuardianDID[g.Did] = true

		if uint64(len(g.Commitments)) > uint64(gs.Params.MaxGuardians) {
			return fmt.Errorf("guardian_sets[%d]: guardian count %d exceeds max_guardians %d",
				i, len(g.Commitments), gs.Params.MaxGuardians)
		}
		if !notTerminated(g.Did) {
			return fmt.Errorf("guardian_sets[%d]: protected did %s must exist and not be revoked", i, g.Did)
		}
	}

	// Recovery requests.
	seenRecoveryID := make(map[string]bool, len(gs.RecoveryRequests))
	openPerDID := make(map[string]uint32, len(gs.RecoveryRequests))
	for i, r := range gs.RecoveryRequests {
		if r.Status != RECOVERY_STATUS_PENDING {
			return fmt.Errorf("recovery_requests[%d]: only PENDING requests may be exported, got %s", i, r.Status)
		}
		if err := ValidateDID(r.Did); err != nil {
			return fmt.Errorf("recovery_requests[%d]: %w", i, err)
		}
		if !notTerminated(r.Did) {
			return fmt.Errorf("recovery_requests[%d]: did %s must exist and not be revoked", i, r.Did)
		}
		if _, err := sdk.AccAddressFromBech32(r.ProposedNewController); err != nil {
			return fmt.Errorf("recovery_requests[%d]: invalid proposed_new_controller: %w", i, err)
		}
		if err := ValidateRecoveryNonce(r.Nonce); err != nil {
			return fmt.Errorf("recovery_requests[%d]: %w", i, err)
		}
		// The curve must be the one execution will actually accept.
		if r.KeyType != KEY_TYPE_SECP256R1 {
			return fmt.Errorf("recovery_requests[%d]: key_type %s is not executable (recovery installs a %s key)",
				i, r.KeyType, KEY_TYPE_SECP256R1)
		}
		// The id must genuinely bind (did, key, nonce) — otherwise a genesis could point one id at a different rotation than the one it commits to.
		if len(r.RecoveryId) != RecoveryIDLen {
			return fmt.Errorf("recovery_requests[%d]: recovery_id length %d, want %d", i, len(r.RecoveryId), RecoveryIDLen)
		}
		want := DeriveRecoveryID(r.Did, r.ProposedNewPubKey, r.Nonce)
		if !subtleEqual(want, r.RecoveryId) {
			return fmt.Errorf("recovery_requests[%d]: recovery_id is not the canonical derivation", i)
		}
		idKey := string(r.RecoveryId)
		if seenRecoveryID[idKey] {
			return fmt.Errorf("duplicate recovery request id at index %d", i)
		}
		seenRecoveryID[idKey] = true

		if r.ExecuteAfter <= r.InitiatedAt || r.ExpiresAt <= r.ExecuteAfter {
			return fmt.Errorf("recovery_requests[%d]: require initiated_at < execute_after < expires_at", i)
		}
		seenApprover := make(map[string]bool, len(r.Approvals))
		for _, a := range r.Approvals {
			if seenApprover[a] {
				return fmt.Errorf("recovery_requests[%d]: duplicate approval from %s", i, a)
			}
			seenApprover[a] = true
		}
		// Rejections are deduped exactly as approvals are: the live handler dedups by revealed guardian DID, so a genesis carrying the same guardian twice would import a tally no handler could produce and would count one human twice toward the threshold that closes the request.
		seenRejector := make(map[string]bool, len(r.Rejections))
		for _, rj := range r.Rejections {
			if seenRejector[rj] {
				return fmt.Errorf("recovery_requests[%d]: duplicate rejection from %s", i, rj)
			}
			seenRejector[rj] = true
		}
		if _, ok := math.NewIntFromString(r.DepositUphi); !ok {
			return fmt.Errorf("recovery_requests[%d]: invalid deposit_uphi %q", i, r.DepositUphi)
		}
		if _, ok := math.NewIntFromString(r.FeeUphi); !ok {
			return fmt.Errorf("recovery_requests[%d]: invalid fee_uphi %q", i, r.FeeUphi)
		}
		// Each method must carry exactly its own authorisation material.
		switch r.Method {
		case RECOVERY_METHOD_SOCIAL:
			if r.AttestorDid != "" {
				return fmt.Errorf("recovery_requests[%d]: a SOCIAL request must not name an attestor", i)
			}
		case RECOVERY_METHOD_REAUTH:
			if len(r.Approvals) != 0 {
				return fmt.Errorf("recovery_requests[%d]: a REAUTH request takes no guardian approvals", i)
			}
			if len(r.Rejections) != 0 {
				return fmt.Errorf("recovery_requests[%d]: a REAUTH request takes no guardian rejections", i)
			}
			if err := ValidateDID(r.AttestorDid); err != nil {
				return fmt.Errorf("recovery_requests[%d]: attestor_did: %w", i, err)
			}
		default:
			return fmt.Errorf("recovery_requests[%d]: method must be SOCIAL or REAUTH, got %s", i, r.Method)
		}
		openPerDID[r.Did]++
		if openPerDID[r.Did] > gs.Params.MaxOpenRecoveryRequests {
			return fmt.Errorf("recovery_requests: did %s has more than max_open_recovery_requests (%d) open requests",
				r.Did, gs.Params.MaxOpenRecoveryRequests)
		}
	}
	return nil
}

func storeEntryRules() []storeentry.Rule {
	return []storeentry.Rule{
		// Single-use markers: the key is the record.
		{Name: "issuer nonce marker", Prefix: IssuerNoncePrefix, Value: storeentry.NonEmpty()},
		{Name: "recovery nonce marker", Prefix: RecoveryNoncePrefix, Value: storeentry.NonEmpty()},
		// Read straight back as the two halves of the unique-DID-per-validator binding.
		{Name: "did→validator binding", Prefix: DIDToValidatorPrefix, Value: validValoperValue},
		{Name: "validator→did binding", Prefix: ValidatorToDIDPrefix, Value: validDIDValue},
		// Bare uint64 counters, and the reason the value check exists at all: a wrong width reads as epoch 0, which un-retires every approval a guardian-set replacement had invalidated.
		{Name: "guardian-set epoch", Prefix: GuardianEpochPrefix, Value: storeentry.Uint64NoOverflow()},
		{Name: "recovery tally epoch", Prefix: RecoveryTallyEpochPrefix, Value: storeentry.Uint64NoOverflow()},
		// (oldest ACTIVE created_at ‖ eligible_since), or the legacy 8-byte form written before the second field existed — which the reader still accepts, so refusing it here would reject a genesis exported from a chain that predates it.
		{Name: "controller eligibility record", Prefix: ControllerEligibilityPrefix, Value: storeentry.OneOfLen(8, 16)},
	}
}

func validValoperValue(v []byte) error {
	if _, err := sdk.ValAddressFromBech32(string(v)); err != nil {
		return fmt.Errorf("value is not a validator operator address: %w", err)
	}
	return nil
}

func validDIDValue(v []byte) error {
	if err := ValidateDID(string(v)); err != nil {
		return fmt.Errorf("value is not a DID: %w", err)
	}
	return nil
}
