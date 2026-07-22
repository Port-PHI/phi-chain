// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/disclosure/types"
)

func (k Keeper) InitGenesis(ctx sdk.Context, gs types.GenesisState) {
	if err := gs.Validate(); err != nil {
		panic(err)
	}
	if err := k.SetParams(ctx, gs.Params); err != nil {
		panic(err)
	}
}

func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	return &types.GenesisState{
		Params: k.GetParams(ctx),
	}
}
