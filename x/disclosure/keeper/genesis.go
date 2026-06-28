// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/disclosure/types"
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
}

// ExportGenesis collects the current state for export.
func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	return &types.GenesisState{
		Params: k.GetParams(ctx),
	}
}
