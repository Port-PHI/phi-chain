// SPDX-License-Identifier: Apache-2.0

package identity

import (
	"context"
	"encoding/json"
	"fmt"

	"cosmossdk.io/core/appmodule"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"

	"github.com/Port-PHI/phi-chain/x/identity/keeper"
	"github.com/Port-PHI/phi-chain/x/identity/types"
)

const ConsensusVersion = 1

var (
	_ module.AppModuleBasic   = AppModuleBasic{}
	_ module.HasGenesis       = AppModule{}
	_ module.HasServices      = AppModule{}
	_ module.HasInvariants    = AppModule{}
	_ appmodule.AppModule     = AppModule{}
	_ appmodule.HasEndBlocker = AppModule{}
)

// AppModuleBasic is the stateless part of the module.
type AppModuleBasic struct {
	cdc codec.Codec
}

func (AppModuleBasic) Name() string { return types.ModuleName }

func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

func (AppModuleBasic) RegisterInterfaces(reg cdctypes.InterfaceRegistry) {
	types.RegisterInterfaces(reg)
}

func (AppModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(types.DefaultGenesis())
}

func (AppModuleBasic) ValidateGenesis(cdc codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var gs types.GenesisState
	if err := cdc.UnmarshalJSON(bz, &gs); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis: %w", types.ModuleName, err)
	}
	return gs.Validate()
}

func (AppModuleBasic) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {
	if err := types.RegisterQueryHandlerClient(context.Background(), mux, types.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
}

// AppModule is the full module.
type AppModule struct {
	AppModuleBasic
	keeper   keeper.Keeper
	staking  keeper.ValidatorSweepStaking
	slashing keeper.ValidatorSweepSlashing
	// genesisStaking is the read-only validator access the founder cross-check uses.
	genesisStaking keeper.ValidatorGenesisStaking
}

// NewAppModule builds the identity module; staking/slashing are narrow interfaces the sweep acts through.
func NewAppModule(cdc codec.Codec, k keeper.Keeper, sk keeper.ValidatorSweepStaking, slk keeper.ValidatorSweepSlashing) AppModule {
	gsk, _ := sk.(keeper.ValidatorGenesisStaking)
	return AppModule{
		AppModuleBasic: AppModuleBasic{cdc: cdc},
		keeper:         k,
		staking:        sk,
		slashing:       slk,
		genesisStaking: gsk,
	}
}

// EndBlock enforces the validator↔DID binding across the active set every block.
func (am AppModule) EndBlock(ctx context.Context) error {
	if am.staking == nil || am.slashing == nil {
		return nil // not wired (unit-test module construction)
	}
	_, _ = am.keeper.SweepValidatorBindings(sdk.UnwrapSDKContext(ctx), am.staking, am.slashing)
	return nil
}

func (AppModule) IsOnePerModuleType() {}

func (AppModule) IsAppModule() {}

func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.keeper))
	types.RegisterQueryServer(cfg.QueryServer(), am.keeper)
}

func (am AppModule) RegisterInvariants(ir sdk.InvariantRegistry) {
	keeper.RegisterInvariants(ir, am.keeper)
}

// InitGenesis initializes module state, then cross-checks it against the founder validator set, panicking on failure (naming the offending operators) rather than starting a doomed chain.
func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, data json.RawMessage) {
	var gs types.GenesisState
	cdc.MustUnmarshalJSON(data, &gs)
	am.keeper.InitGenesis(ctx, gs)

	if am.genesisStaking == nil {
		return // not wired (unit-test module construction)
	}
	if err := am.keeper.ValidateGenesisFounders(ctx, am.genesisStaking); err != nil {
		panic(err)
	}
}

func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(am.keeper.ExportGenesis(ctx))
}

func (AppModule) ConsensusVersion() uint64 { return ConsensusVersion }
