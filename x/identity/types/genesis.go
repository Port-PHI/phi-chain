// SPDX-License-Identifier: Apache-2.0

package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultGenesis returns the default genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:         DefaultParams(),
		Identities:     []DIDDocument{},
		IdentityCount:  0,
		TrustedIssuers: []TrustedIssuer{},
	}
}

// Validate checks the genesis state for correctness.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	// Uniqueness of DID and biometric marker in genesis.
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

		// Mirror the runtime's *immutable* identity invariants so a curated, exported, or
		// compromised genesis cannot seed controller-spoofed or malformed DIDs that the governance/voting
		// tally would then treat as eligible humans. The anti-Sybil anchor enforced here is the
		// uniqueness-marker ↔ DID relation (one human → one marker → one DID), validated below; that is the
		// binding RotateIdentityKey holds invariant. Self-certification (did == DeriveDIDFromP256(pub_key))
		// is deliberately NOT required at genesis: RotateIdentityKey keeps the DID stable while replacing
		// pub_key, so a rotated identity has did != DeriveDIDFromP256(pub_key) by design. Requiring equality
		// here would make ExportGenesis of any rotated identity fail its own ValidateGenesis and panic
		// InitGenesis — breaking the export/import upgrade and recovery path. Self-cert is still enforced at
		// registration time (keeper.RegisterIdentity), which is where a fresh DID is minted.
		// The JSON ValidateGenesis path and programmatic InitGenesis both run this.
		if len(d.PubKey) == 0 || len(d.PubKey) > MaxPubKeyLen {
			return fmt.Errorf("identity %s: pub_key length %d (must be 1..%d)", d.Did, len(d.PubKey), MaxPubKeyLen)
		}
		if _, err := sdk.AccAddressFromBech32(d.Controller); err != nil {
			return fmt.Errorf("identity %s: invalid controller address %q: %w", d.Did, d.Controller, err)
		}
		if d.Status != DID_STATUS_ACTIVE && d.Status != DID_STATUS_REVOKED {
			return fmt.Errorf("identity %s: unknown status %d (must be ACTIVE or REVOKED)", d.Did, d.Status)
		}

		// An empty uniqueness marker would bind to the bare key prefix and silently bypass
		// the one-human-one-DID guarantee; reject it outright.
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
	// Trusted issuers: unique, syntactically valid DID, and a non-empty public key.
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
	// Raw marker round-trip: each StoreEntry key must carry one of the identity marker
	// prefixes (issuer nonce, did→validator, validator→did), confining an imported genesis to those
	// keyspaces so it cannot overwrite a DIDDocument, the params, or the uniqueness/controller indexes.
	allowed := [][]byte{IssuerNoncePrefix, DIDToValidatorPrefix, ValidatorToDIDPrefix}
	for i, e := range gs.StoreEntries {
		ok := false
		for _, p := range allowed {
			if len(e.Key) >= len(p) && string(e.Key[:len(p)]) == string(p) {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("store_entry[%d]: key is not under an allowed identity marker prefix", i)
		}
	}

	// Validator↔DID binding: the did→valoper and valoper→did marker entries must be a
	// bijection (each is the other's inverse, so a DID binds to at most one validator and vice versa),
	// and every bound DID must exist and be ACTIVE — the anti-Sybil anchor that one validator maps to
	// one live human. A curated/compromised genesis cannot otherwise bind a validator to a missing,
	// revoked, or duplicate DID.
	status := make(map[string]DIDStatus, len(gs.Identities))
	for _, d := range gs.Identities {
		status[d.Did] = d.Status
	}
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
		if st, ok := status[did]; !ok || st != DID_STATUS_ACTIVE {
			return fmt.Errorf("validator-bound did %s must exist and be ACTIVE", did)
		}
	}
	for valoper, did := range valToDID {
		if didToVal[did] != valoper {
			return fmt.Errorf("validator binding not bijective: %s → %s, but did %s → %q", valoper, did, did, didToVal[did])
		}
	}
	return nil
}
