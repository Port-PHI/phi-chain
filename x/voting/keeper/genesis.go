// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/voting/types"
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
	for _, e := range gs.Elections {
		k.SetElection(ctx, e)
	}
	for _, b := range gs.Ballots {
		k.SetBallot(ctx, b)
	}
}

// ExportGenesis collects the current state for export.
func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	gs := &types.GenesisState{
		Params:    k.GetParams(ctx),
		Elections: []types.Election{},
		Ballots:   []types.Ballot{},
	}
	k.IterateElections(ctx, func(e types.Election) bool {
		gs.Elections = append(gs.Elections, e)
		return false
	})
	k.IterateBallots(ctx, func(b types.Ballot) bool {
		gs.Ballots = append(gs.Ballots, b)
		return false
	})
	return gs
}
