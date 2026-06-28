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
	// MaxValidators is the cap on the active validator set (7-10).
	MaxValidators = 10
	// DefaultBlockMaxGas bounds the total gas a block may consume. CometBFT's default is -1
	// (unlimited); with a fixed per-message fee that does not price gas, an unlimited block lets a tx
	// buy unbounded validator compute. The InitChainer caps an unlimited genesis value to this finite
	// default, so a maximally expensive single-message tx (gas > MaxGas) is rejected by the block gas
	// meter. Generous enough for many ordinary txs per block (each ≪ MaxTxGas).
	DefaultBlockMaxGas = int64(100_000_000)
)

// fullBasic is the full interface of a basic module (including genesis), for embedding in an override.
type fullBasic interface {
	module.AppModuleBasic
	module.HasGenesisBasics
}

// genesisOverride wraps an AppModuleBasic with a custom default genesis.
// (ValidateGenesis is promoted from fullBasic; only DefaultGenesis is overridden.)
type genesisOverride struct {
	fullBasic
	defaultGenesis func(codec.JSONCodec) json.RawMessage
}

func (g genesisOverride) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return g.defaultGenesis(cdc)
}

// phiStakingGenesis sets the bond denom to uphi, 14-day unbonding, set cap of 10, and 5% minimum commission.
func phiStakingGenesis(cdc codec.JSONCodec) json.RawMessage {
	gs := stakingtypes.DefaultGenesisState()
	gs.Params.BondDenom = cointypes.Denom
	gs.Params.UnbondingTime = UnbondingTime
	gs.Params.MaxValidators = MaxValidators
	gs.Params.MinCommissionRate = math.LegacyNewDecWithPrec(5, 2)
	return cdc.MustMarshalJSON(gs)
}

// phiSlashingGenesis: 5% double-sign slash + tombstone, 0.1% downtime slash + jail.
func phiSlashingGenesis(cdc codec.JSONCodec) json.RawMessage {
	gs := slashingtypes.DefaultGenesisState()
	gs.Params.SlashFractionDoubleSign = math.LegacyNewDecWithPrec(5, 2) // 5%
	gs.Params.SlashFractionDowntime = math.LegacyNewDecWithPrec(1, 3)   // 0.1%
	gs.Params.DowntimeJailDuration = 10 * time.Minute
	gs.Params.SignedBlocksWindow = 10_000
	gs.Params.MinSignedPerWindow = math.LegacyNewDecWithPrec(5, 2)
	return cdc.MustMarshalJSON(gs)
}

// phiCrisisGenesis sets the fixed invariant-check fee in uphi.
func phiCrisisGenesis(cdc codec.JSONCodec) json.RawMessage {
	gs := crisistypes.DefaultGenesisState()
	gs.ConstantFee = sdk.NewCoin(cointypes.Denom, math.NewInt(1000))
	return cdc.MustMarshalJSON(gs)
}

// phiGovGenesis sets the proposal deposit in uphi (10 PHI / 50 PHI expedited) and makes governance
// deposit handling supply-neutral. The bond/gov denom IS the vault-backed uphi, so burning a
// deposit would shrink supply and break solvency (supply×phi_to_toman == Σvault×1e6). Instead of
// burning, vetoed/failed deposits are refunded and the cancellation share is routed to the
// governance account — total supply is unchanged in every case.
func phiGovGenesis(cdc codec.JSONCodec) json.RawMessage {
	gs := govv1.DefaultGenesisState()
	gs.Params.MinDeposit = sdk.NewCoins(sdk.NewCoin(cointypes.Denom, math.NewInt(10_000_000)))
	gs.Params.ExpeditedMinDeposit = sdk.NewCoins(sdk.NewCoin(cointypes.Denom, math.NewInt(50_000_000)))
	// Do not burn deposits (supply-preserving): veto and failed-prevote deposits are refunded.
	gs.Params.BurnVoteVeto = false
	gs.Params.BurnProposalDepositPrevote = false
	// Cancellation share is sent to the governance account instead of being burned (PenaltyDestination
	// resolves to this same governance account by default).
	gs.Params.ProposalCancelDest = authtypes.NewModuleAddress(govtypes.ModuleName).String()
	return cdc.MustMarshalJSON(gs)
}
