// SPDX-License-Identifier: Apache-2.0

package app

import (
	"encoding/json"
	"time"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	crisistypes "github.com/cosmos/cosmos-sdk/x/crisis/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
)

// Base Phi staking/governance parameters in genesis.
const (
	// UnbondingTime is the unbonding period: 14 days.
	UnbondingTime = 14 * 24 * time.Hour
	// MaxValidators caps the active validator set (10); no hard floor (CometBFT >2/3 stake-weight liveness).
	MaxValidators = 10
	// DefaultBlockMaxGas caps total block gas: with a fixed per-message fee, an unlimited block would let a tx buy unbounded compute.
	DefaultBlockMaxGas = int64(100_000_000)
	// InvariantCheckFeeUphi is the fixed fee for a permissionless invariant check: 1 PHI (200x a transfer).
	InvariantCheckFeeUphi = int64(1_000_000)
)

type fullBasic interface {
	module.AppModuleBasic
	module.HasGenesisBasics
}

type genesisOverride struct {
	fullBasic
	defaultGenesis func(codec.JSONCodec) json.RawMessage
}

func (g genesisOverride) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return g.defaultGenesis(cdc)
}

func phiStakingGenesis(cdc codec.JSONCodec) json.RawMessage {
	gs := stakingtypes.DefaultGenesisState()
	gs.Params.BondDenom = cointypes.Denom
	gs.Params.UnbondingTime = UnbondingTime
	gs.Params.MaxValidators = MaxValidators
	gs.Params.MinCommissionRate = math.LegacyNewDecWithPrec(5, 2)
	return cdc.MustMarshalJSON(gs)
}

func phiSlashingGenesis(cdc codec.JSONCodec) json.RawMessage {
	gs := slashingtypes.DefaultGenesisState()
	gs.Params.SlashFractionDoubleSign = math.LegacyNewDecWithPrec(5, 2)
	gs.Params.SlashFractionDowntime = math.LegacyNewDecWithPrec(1, 3)
	gs.Params.DowntimeJailDuration = 10 * time.Minute
	gs.Params.SignedBlocksWindow = 10_000
	gs.Params.MinSignedPerWindow = math.LegacyNewDecWithPrec(5, 2)
	return cdc.MustMarshalJSON(gs)
}

func phiCrisisGenesis(cdc codec.JSONCodec) json.RawMessage {
	gs := crisistypes.DefaultGenesisState()
	gs.ConstantFee = sdk.NewCoin(cointypes.Denom, math.NewInt(InvariantCheckFeeUphi))
	return cdc.MustMarshalJSON(gs)
}

func phiGovGenesis(cdc codec.JSONCodec) json.RawMessage {
	gs := govv1.DefaultGenesisState()
	gs.Params.MinDeposit = sdk.NewCoins(sdk.NewCoin(cointypes.Denom, math.NewInt(10_000_000)))
	gs.Params.ExpeditedMinDeposit = sdk.NewCoins(sdk.NewCoin(cointypes.Denom, math.NewInt(50_000_000)))
	// All three burn flags pinned false explicitly: an SDK default flipping one true would burn a vault-backed deposit and halt.
	gs.Params.BurnVoteVeto = false
	gs.Params.BurnProposalDepositPrevote = false
	gs.Params.BurnVoteQuorum = false
	// Cancellation share is sent to the governance account instead of being burned.
	gs.Params.ProposalCancelDest = authtypes.NewModuleAddress(govtypes.ModuleName).String()
	return cdc.MustMarshalJSON(gs)
}
