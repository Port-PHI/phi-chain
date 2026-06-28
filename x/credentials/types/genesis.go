// SPDX-License-Identifier: Apache-2.0

package types

import (
	"encoding/hex"
	"fmt"
)

// DefaultGenesis returns the default genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:          DefaultParams(),
		Templates:       []CredentialTemplate{},
		Anchors:         []CredentialAnchor{},
		Agreements:      []Agreement{},
		PersonalAnchors: []PersonalAnchor{},
	}
}

// Validate checks the genesis state for consistency.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}

	templates := make(map[string]CredentialTemplate)
	for _, t := range gs.Templates {
		if t.Id == "" {
			return fmt.Errorf("template with empty id")
		}
		if _, dup := templates[t.Id]; dup {
			return fmt.Errorf("duplicate template id in genesis: %s", t.Id)
		}
		templates[t.Id] = t
	}

	seenAnchor := make(map[string]bool)
	for _, a := range gs.Anchors {
		if len(a.CredentialHash) == 0 {
			return fmt.Errorf("credential anchor with empty hash")
		}
		key := hex.EncodeToString(a.CredentialHash)
		if seenAnchor[key] {
			return fmt.Errorf("duplicate credential anchor in genesis: %s", key)
		}
		seenAnchor[key] = true
		if t, ok := templates[a.TemplateId]; !ok {
			return fmt.Errorf("anchor %s references unknown template %q", key, a.TemplateId)
		} else if a.TemplateVersion > t.Version {
			return fmt.Errorf("anchor %s references future template version %d (current %d)", key, a.TemplateVersion, t.Version)
		}
	}

	seenAgreement := make(map[string]bool)
	for _, ag := range gs.Agreements {
		if len(ag.Hash) == 0 {
			return fmt.Errorf("agreement with empty hash")
		}
		key := hex.EncodeToString(ag.Hash)
		if seenAgreement[key] {
			return fmt.Errorf("duplicate agreement in genesis: %s", key)
		}
		seenAgreement[key] = true
		if len(ag.RequiredSigners) == 0 {
			return fmt.Errorf("agreement %s has no required signers", key)
		}
		if uint32(len(ag.RequiredSigners)) > gs.Params.MaxAgreementSigners {
			return fmt.Errorf("agreement %s exceeds max_agreement_signers", key)
		}
		required := make(map[string]bool, len(ag.RequiredSigners))
		for _, did := range ag.RequiredSigners {
			required[did] = true
		}
		signed := make(map[string]bool, len(ag.Signatures))
		for _, s := range ag.Signatures {
			if !required[s.SignerDid] {
				return fmt.Errorf("agreement %s signed by non-required DID %s", key, s.SignerDid)
			}
			if signed[s.SignerDid] {
				return fmt.Errorf("agreement %s signed twice by DID %s", key, s.SignerDid)
			}
			signed[s.SignerDid] = true
		}
	}

	seenPersonal := make(map[string]bool)
	for _, p := range gs.PersonalAnchors {
		if p.OwnerDid == "" || len(p.AnchorHash) == 0 {
			return fmt.Errorf("personal anchor with empty owner or hash")
		}
		key := p.OwnerDid + "/" + hex.EncodeToString(p.AnchorHash)
		if seenPersonal[key] {
			return fmt.Errorf("duplicate personal anchor in genesis: %s", key)
		}
		seenPersonal[key] = true
	}

	return nil
}
