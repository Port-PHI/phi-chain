// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/credentials/types"
)

// InitGenesis loads the genesis state.
func (k Keeper) InitGenesis(ctx sdk.Context, gs types.GenesisState) {
	// Re-run structural validation; the JSON-path ValidateGenesis is not guaranteed for
	// programmatically constructed genesis (upgrades, tests, app-wired init).
	if err := gs.Validate(); err != nil {
		panic(err)
	}
	if err := k.SetParams(ctx, gs.Params); err != nil {
		panic(err)
	}
	for _, t := range gs.Templates {
		k.SetTemplate(ctx, t)
	}
	for _, a := range gs.Anchors {
		k.SetAnchor(ctx, a)
	}
	for _, ag := range gs.Agreements {
		k.SetAgreement(ctx, ag)
	}
	for _, p := range gs.PersonalAnchors {
		k.SetPersonalAnchor(ctx, p)
	}
}

// ExportGenesis collects the current state for export.
func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	gs := &types.GenesisState{
		Params:          k.GetParams(ctx),
		Templates:       []types.CredentialTemplate{},
		Anchors:         []types.CredentialAnchor{},
		Agreements:      []types.Agreement{},
		PersonalAnchors: []types.PersonalAnchor{},
	}
	k.IterateTemplates(ctx, func(t types.CredentialTemplate) bool {
		gs.Templates = append(gs.Templates, t)
		return false
	})
	k.IterateAnchors(ctx, func(a types.CredentialAnchor) bool {
		gs.Anchors = append(gs.Anchors, a)
		return false
	})
	k.IterateAgreements(ctx, func(a types.Agreement) bool {
		gs.Agreements = append(gs.Agreements, a)
		return false
	})
	k.IteratePersonalAnchors(ctx, func(p types.PersonalAnchor) bool {
		gs.PersonalAnchors = append(gs.PersonalAnchors, p)
		return false
	})
	return gs
}
