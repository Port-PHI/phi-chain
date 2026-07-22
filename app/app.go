// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
	reflectionv1 "cosmossdk.io/api/cosmos/reflection/v1"
	"cosmossdk.io/client/v2/autocli"
	clienthelpers "cosmossdk.io/client/v2/helpers"
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/log"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"cosmossdk.io/x/evidence"
	evidencekeeper "cosmossdk.io/x/evidence/keeper"
	evidencetypes "cosmossdk.io/x/evidence/types"
	"cosmossdk.io/x/feegrant"
	feegrantkeeper "cosmossdk.io/x/feegrant/keeper"
	feegrantmodule "cosmossdk.io/x/feegrant/module"
	"cosmossdk.io/x/tx/signing"
	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/gogoproto/proto"
	"github.com/spf13/cast"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	nodeservice "github.com/cosmos/cosmos-sdk/client/grpc/node"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/address"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	runtimeservices "github.com/cosmos/cosmos-sdk/runtime/services"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/api"
	"github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/std"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/cosmos/cosmos-sdk/x/auth"
	authcodec "github.com/cosmos/cosmos-sdk/x/auth/codec"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authsims "github.com/cosmos/cosmos-sdk/x/auth/simulation"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/bank"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/consensus"
	consensusparamkeeper "github.com/cosmos/cosmos-sdk/x/consensus/keeper"
	consensusparamtypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	"github.com/cosmos/cosmos-sdk/x/crisis"
	crisiskeeper "github.com/cosmos/cosmos-sdk/x/crisis/keeper"
	crisistypes "github.com/cosmos/cosmos-sdk/x/crisis/types"
	distr "github.com/cosmos/cosmos-sdk/x/distribution"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/cosmos/cosmos-sdk/x/gov"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	"github.com/cosmos/cosmos-sdk/x/params"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/cosmos/cosmos-sdk/x/slashing"
	slashingkeeper "github.com/cosmos/cosmos-sdk/x/slashing/keeper"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	phiante "github.com/Port-PHI/phi-chain/app/ante"
	"github.com/Port-PHI/phi-chain/phicrypto"
	"github.com/Port-PHI/phi-chain/x/coin"
	coinkeeper "github.com/Port-PHI/phi-chain/x/coin/keeper"
	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	"github.com/Port-PHI/phi-chain/x/credentials"
	credentialskeeper "github.com/Port-PHI/phi-chain/x/credentials/keeper"
	credentialstypes "github.com/Port-PHI/phi-chain/x/credentials/types"
	"github.com/Port-PHI/phi-chain/x/disclosure"
	disclosurekeeper "github.com/Port-PHI/phi-chain/x/disclosure/keeper"
	disclosuretypes "github.com/Port-PHI/phi-chain/x/disclosure/types"
	"github.com/Port-PHI/phi-chain/x/governance"
	governancekeeper "github.com/Port-PHI/phi-chain/x/governance/keeper"
	governancetypes "github.com/Port-PHI/phi-chain/x/governance/types"
	"github.com/Port-PHI/phi-chain/x/identity"
	identitykeeper "github.com/Port-PHI/phi-chain/x/identity/keeper"
	identitytypes "github.com/Port-PHI/phi-chain/x/identity/types"
	"github.com/Port-PHI/phi-chain/x/institutions"
	institutionskeeper "github.com/Port-PHI/phi-chain/x/institutions/keeper"
	institutionstypes "github.com/Port-PHI/phi-chain/x/institutions/types"
	"github.com/Port-PHI/phi-chain/x/voting"
	votingkeeper "github.com/Port-PHI/phi-chain/x/voting/keeper"
	votingtypes "github.com/Port-PHI/phi-chain/x/voting/types"
)

const (
	AppName              = "phi"
	AccountAddressPrefix = "phi"
)

var (
	DefaultNodeHome string

	maccPerms = map[string][]string{
		authtypes.FeeCollectorName:     nil,
		distrtypes.ModuleName:          nil,
		stakingtypes.BondedPoolName:    {authtypes.Burner, authtypes.Staking},
		stakingtypes.NotBondedPoolName: {authtypes.Burner, authtypes.Staking},
		govtypes.ModuleName:            {authtypes.Burner},
		// Institutions mint/burn only against their own vault.
		institutionstypes.ModuleName: {authtypes.Minter, authtypes.Burner},
		// Identity holds NO Minter/Burner: a forfeited deposit is moved to the fee collector, never burned (solvency).
		identitytypes.ModuleName: nil,
		// phi_revenue: keyless, no permissions; only receives fee-split, emptied by gov MsgWithdrawRevenue; blocked address.
		cointypes.RevenueAccountName: nil,
	}
)

var (
	_ runtime.AppI            = (*App)(nil)
	_ servertypes.Application = (*App)(nil)
)

// App is the Phi chain application.
type App struct {
	*baseapp.BaseApp
	legacyAmino       *codec.LegacyAmino
	appCodec          codec.Codec
	txConfig          client.TxConfig
	interfaceRegistry codectypes.InterfaceRegistry

	keys  map[string]*storetypes.KVStoreKey
	tkeys map[string]*storetypes.TransientStoreKey

	AccountKeeper         authkeeper.AccountKeeper
	BankKeeper            bankkeeper.BaseKeeper
	StakingKeeper         *stakingkeeper.Keeper
	SlashingKeeper        slashingkeeper.Keeper
	DistrKeeper           distrkeeper.Keeper
	GovKeeper             govkeeper.Keeper
	CrisisKeeper          *crisiskeeper.Keeper
	ParamsKeeper          paramskeeper.Keeper
	FeeGrantKeeper        feegrantkeeper.Keeper
	EvidenceKeeper        evidencekeeper.Keeper
	ConsensusParamsKeeper consensusparamkeeper.Keeper

	IdentityKeeper     identitykeeper.Keeper
	CoinKeeper         coinkeeper.Keeper
	InstitutionsKeeper institutionskeeper.Keeper
	CredentialsKeeper  credentialskeeper.Keeper
	DisclosureKeeper   disclosurekeeper.Keeper
	VotingKeeper       votingkeeper.Keeper
	GovernanceKeeper   governancekeeper.Keeper

	ModuleManager      *module.Manager
	BasicModuleManager module.BasicManager
	configurator       module.Configurator
}

func init() {
	var err error
	DefaultNodeHome, err = clienthelpers.GetNodeHomeDirectory(".phid")
	if err != nil {
		panic(err)
	}
}

// NewApp builds an App; enforceCrypto=true refuses the fail-closed Disabled verifier (build without -tags phicrypto_cgo) so a tagless binary cannot run a node.
func NewApp(
	logger log.Logger,
	db dbm.DB,
	traceStore io.Writer,
	loadLatest bool,
	appOpts servertypes.AppOptions,
	enforceCrypto bool,
	baseAppOptions ...func(*baseapp.BaseApp),
) *App {
	interfaceRegistry, _ := codectypes.NewInterfaceRegistryWithOptions(codectypes.InterfaceRegistryOptions{
		ProtoFiles: proto.HybridResolver,
		SigningOptions: signing.Options{
			AddressCodec:          address.Bech32Codec{Bech32Prefix: sdk.GetConfig().GetBech32AccountAddrPrefix()},
			ValidatorAddressCodec: address.Bech32Codec{Bech32Prefix: sdk.GetConfig().GetBech32ValidatorAddrPrefix()},
		},
	})
	appCodec := codec.NewProtoCodec(interfaceRegistry)
	legacyAmino := codec.NewLegacyAmino()
	txConfig := authtx.NewTxConfig(appCodec, authtx.DefaultSignModes)

	if err := interfaceRegistry.SigningContext().Validate(); err != nil {
		panic(err)
	}

	std.RegisterLegacyAminoCodec(legacyAmino)
	std.RegisterInterfaces(interfaceRegistry)

	bApp := baseapp.NewBaseApp(AppName, logger, db, txConfig.TxDecoder(), baseAppOptions...)
	bApp.SetCommitMultiStoreTracer(traceStore)
	bApp.SetVersion(version.Version)
	bApp.SetInterfaceRegistry(interfaceRegistry)
	bApp.SetTxEncoder(txConfig.TxEncoder())

	keys := storetypes.NewKVStoreKeys(
		authtypes.StoreKey, banktypes.StoreKey, stakingtypes.StoreKey, crisistypes.StoreKey,
		distrtypes.StoreKey, slashingtypes.StoreKey, govtypes.StoreKey,
		paramstypes.StoreKey, consensusparamtypes.StoreKey, feegrant.StoreKey, evidencetypes.StoreKey,
		identitytypes.StoreKey, cointypes.StoreKey, institutionstypes.StoreKey,
		credentialstypes.StoreKey, disclosuretypes.StoreKey, votingtypes.StoreKey,
		governancetypes.StoreKey,
	)
	tkeys := storetypes.NewTransientStoreKeys(paramstypes.TStoreKey)

	app := &App{
		BaseApp:           bApp,
		legacyAmino:       legacyAmino,
		appCodec:          appCodec,
		txConfig:          txConfig,
		interfaceRegistry: interfaceRegistry,
		keys:              keys,
		tkeys:             tkeys,
	}

	govModAddr := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	app.ParamsKeeper = initParamsKeeper(appCodec, legacyAmino, keys[paramstypes.StoreKey], tkeys[paramstypes.TStoreKey])

	app.ConsensusParamsKeeper = consensusparamkeeper.NewKeeper(appCodec, runtime.NewKVStoreService(keys[consensusparamtypes.StoreKey]), govModAddr, runtime.EventService{})
	bApp.SetParamStore(app.ConsensusParamsKeeper.ParamsStore)

	app.AccountKeeper = authkeeper.NewAccountKeeper(appCodec, runtime.NewKVStoreService(keys[authtypes.StoreKey]), authtypes.ProtoBaseAccount, maccPerms, authcodec.NewBech32Codec(AccountAddressPrefix), AccountAddressPrefix, govModAddr)

	app.BankKeeper = bankkeeper.NewBaseKeeper(appCodec, runtime.NewKVStoreService(keys[banktypes.StoreKey]), app.AccountKeeper, app.BlockedAddresses(), govModAddr, logger)

	app.StakingKeeper = stakingkeeper.NewKeeper(appCodec, runtime.NewKVStoreService(keys[stakingtypes.StoreKey]), app.AccountKeeper, app.BankKeeper, govModAddr, authcodec.NewBech32Codec(sdk.GetConfig().GetBech32ValidatorAddrPrefix()), authcodec.NewBech32Codec(sdk.GetConfig().GetBech32ConsensusAddrPrefix()))

	app.DistrKeeper = distrkeeper.NewKeeper(appCodec, runtime.NewKVStoreService(keys[distrtypes.StoreKey]), app.AccountKeeper, app.BankKeeper, app.StakingKeeper, authtypes.FeeCollectorName, govModAddr)

	// Wrap the staking keeper so the whole slash is re-minted to the penalty destination, keeping uphi supply constant.
	app.SlashingKeeper = slashingkeeper.NewKeeper(appCodec, legacyAmino, runtime.NewKVStoreService(keys[slashingtypes.StoreKey]),
		newSlashCompensator(app.StakingKeeper, app.BankKeeper, &app.InstitutionsKeeper), govModAddr)

	invCheckPeriod := cast.ToUint(appOpts.Get(server.FlagInvCheckPeriod))
	app.CrisisKeeper = crisiskeeper.NewKeeper(appCodec, runtime.NewKVStoreService(keys[crisistypes.StoreKey]), invCheckPeriod, app.BankKeeper, authtypes.FeeCollectorName, govModAddr, app.AccountKeeper.AddressCodec())

	app.FeeGrantKeeper = feegrantkeeper.NewKeeper(appCodec, runtime.NewKVStoreService(keys[feegrant.StoreKey]), app.AccountKeeper)

	// x/evidence slashes+jails+tombstones equivocation; the burn flows through the compensation wrapper (supply-neutral).
	evidenceKeeper := evidencekeeper.NewKeeper(appCodec, runtime.NewKVStoreService(keys[evidencetypes.StoreKey]),
		app.StakingKeeper, app.SlashingKeeper, app.AccountKeeper.AddressCodec(), runtime.ProvideCometInfoService())
	app.EvidenceKeeper = *evidenceKeeper

	// Phi keepers (before staking hooks: the validator hook depends on IdentityKeeper).
	app.IdentityKeeper = identitykeeper.NewKeeper(appCodec, keys[identitytypes.StoreKey], govModAddr, phicrypto.Default(), app.BankKeeper)
	app.CoinKeeper = coinkeeper.NewKeeper(appCodec, keys[cointypes.StoreKey], govModAddr, app.BankKeeper, app.IdentityKeeper)
	app.InstitutionsKeeper = institutionskeeper.NewKeeper(appCodec, keys[institutionstypes.StoreKey], govModAddr, app.BankKeeper, app.IdentityKeeper, app.CoinKeeper, phicrypto.Default())
	// credentials: BBS+ verify via phicrypto.Default() (Disabled/fail-safe until -tags phicrypto_cgo).
	app.CredentialsKeeper = credentialskeeper.NewKeeper(appCodec, keys[credentialstypes.StoreKey], govModAddr, app.IdentityKeeper, phicrypto.Default())
	// disclosure: verify-only BBS+ selective-disclosure; no per-disclosure state.
	app.DisclosureKeeper = disclosurekeeper.NewKeeper(appCodec, keys[disclosuretypes.StoreKey], govModAddr, app.CredentialsKeeper, phicrypto.Default())
	// voting: anonymous, BBS+ eligibility + per-election nullifier dedup.
	app.VotingKeeper = votingkeeper.NewKeeper(appCodec, keys[votingtypes.StoreKey], govModAddr, app.CredentialsKeeper, phicrypto.Default(), votingkeeper.VotingSoundnessEnforced)

	// Staking hooks: distr + slashing + Phi validator rule (unique DID + min 1000 PHI self-stake).
	minSelfStake := math.NewInt(1000).Mul(math.NewIntFromUint64(cointypes.UphiPerPhi))
	app.StakingKeeper.SetHooks(stakingtypes.NewMultiStakingHooks(
		app.DistrKeeper.Hooks(),
		app.SlashingKeeper.Hooks(),
		app.IdentityKeeper.NewValidatorHooks(app.StakingKeeper, minSelfStake),
	))

	// Vote-route table, built before the gov keeper (the tally reads it); governed by the public path only (§6.1).
	app.GovernanceKeeper = governancekeeper.NewKeeper(appCodec, keys[governancetypes.StoreKey], govModAddr)

	govRouter := govv1beta1.NewRouter()
	govRouter.AddRoute(govtypes.RouterKey, govv1beta1.ProposalHandler)
	govConfig := govtypes.DefaultConfig()
	// Custom public/technical tally via the Cosmos hook; route from the governed table, except a mapping-rewrite proposal hard-classified PUBLIC (anti-capture).
	govBank := newGovBurnGuard(app.BankKeeper, &app.InstitutionsKeeper)
	govKeeper := govkeeper.NewKeeper(appCodec, runtime.NewKVStoreService(keys[govtypes.StoreKey]), app.AccountKeeper, govBank, app.StakingKeeper, app.DistrKeeper, app.MsgServiceRouter(), govConfig, govModAddr,
		govkeeper.WithCustomCalculateVoteResultsAndVotingPowerFn(governance.NewPhiTallyFn(app.GovernanceKeeper, app.IdentityKeeper, app.GovernanceKeeper)))
	govKeeper.SetLegacyRouter(govRouter)
	// Public tally accumulates via gov hooks so the EndBlocker reads a finished result.
	app.GovKeeper = *govKeeper.SetHooks(govtypes.NewMultiGovHooks(
		governance.NewVoteHooks(app.GovernanceKeeper, &app.GovKeeper, app.IdentityKeeper,
			app.GovernanceKeeper, governance.NewStakingValidatorSource(app.StakingKeeper)),
	))

	// Inject gov keeper into institutions (fx onboarding needs a PASSED public proposal); before the AppModule copies the keeper by value.
	app.InstitutionsKeeper.SetGovKeeper(govProposalAdapter{gk: &app.GovKeeper})

	app.ModuleManager = module.NewManager(
		genutil.NewAppModule(app.AccountKeeper, app.StakingKeeper, app, txConfig),
		auth.NewAppModule(appCodec, app.AccountKeeper, authsims.RandomGenesisAccounts, app.GetSubspace(authtypes.ModuleName)),
		bank.NewAppModule(appCodec, app.BankKeeper, app.AccountKeeper, app.GetSubspace(banktypes.ModuleName)),
		staking.NewAppModule(appCodec, app.StakingKeeper, app.AccountKeeper, app.BankKeeper, app.GetSubspace(stakingtypes.ModuleName)),
		distr.NewAppModule(appCodec, app.DistrKeeper, app.AccountKeeper, app.BankKeeper, app.StakingKeeper, app.GetSubspace(distrtypes.ModuleName)),
		slashing.NewAppModule(appCodec, app.SlashingKeeper, app.AccountKeeper, app.BankKeeper, app.StakingKeeper, app.GetSubspace(slashingtypes.ModuleName), app.interfaceRegistry),
		evidence.NewAppModule(app.EvidenceKeeper),
		gov.NewAppModule(appCodec, &app.GovKeeper, app.AccountKeeper, app.BankKeeper, app.GetSubspace(govtypes.ModuleName)),
		crisis.NewAppModule(app.CrisisKeeper, cast.ToBool(appOpts.Get(crisis.FlagSkipGenesisInvariants)), app.GetSubspace(crisistypes.ModuleName)),
		feegrantmodule.NewAppModule(appCodec, app.AccountKeeper, app.BankKeeper, app.FeeGrantKeeper, app.interfaceRegistry),
		params.NewAppModule(app.ParamsKeeper),
		consensus.NewAppModule(appCodec, app.ConsensusParamsKeeper),
		identity.NewAppModule(appCodec, app.IdentityKeeper, app.StakingKeeper, app.SlashingKeeper),
		coin.NewAppModule(appCodec, app.CoinKeeper),
		institutions.NewAppModule(appCodec, app.InstitutionsKeeper),
		credentials.NewAppModule(appCodec, app.CredentialsKeeper),
		disclosure.NewAppModule(appCodec, app.DisclosureKeeper),
		voting.NewAppModule(appCodec, app.VotingKeeper),
		governance.NewAppModule(appCodec, app.GovernanceKeeper),
	)

	app.BasicModuleManager = module.NewBasicManagerFromManager(app.ModuleManager, map[string]module.AppModuleBasic{
		genutiltypes.ModuleName:  genutil.NewAppModuleBasic(genutiltypes.DefaultMessageValidator),
		stakingtypes.ModuleName:  genesisOverride{staking.AppModuleBasic{}, phiStakingGenesis},
		slashingtypes.ModuleName: genesisOverride{slashing.AppModuleBasic{}, phiSlashingGenesis},
		crisistypes.ModuleName:   genesisOverride{crisis.AppModuleBasic{}, phiCrisisGenesis},
		govtypes.ModuleName:      genesisOverride{gov.NewAppModuleBasic(nil), phiGovGenesis},
	})
	app.BasicModuleManager.RegisterLegacyAminoCodec(legacyAmino)
	app.BasicModuleManager.RegisterInterfaces(interfaceRegistry)

	app.ModuleManager.SetOrderBeginBlockers(
		// evidence before slashing/staking: an equivocation is slashed+jailed+tombstoned this block, compensated.
		distrtypes.ModuleName, evidencetypes.ModuleName, slashingtypes.ModuleName, stakingtypes.ModuleName, genutiltypes.ModuleName,
		// coin: prunes stale daily micro-exemption quota keys.
		cointypes.ModuleName,
		// institutions: prunes past-day daily cap counters.
		institutionstypes.ModuleName,
	)
	app.ModuleManager.SetOrderEndBlockers(
		crisistypes.ModuleName, govtypes.ModuleName,
		// identity sweeps validator↔DID binding before staking so a jailed validator leaves the set this block.
		identitytypes.ModuleName,
		stakingtypes.ModuleName, genutiltypes.ModuleName, feegrant.ModuleName,
		// institutions asserts solvency invariants in EndBlock, after staking so this block's slash is compensated.
		institutionstypes.ModuleName,
		// governance prunes stale vote records after gov tally, per-block budget.
		governancetypes.ModuleName,
	)

	genesisModuleOrder := []string{
		authtypes.ModuleName, banktypes.ModuleName,
		distrtypes.ModuleName, stakingtypes.ModuleName, slashingtypes.ModuleName, evidencetypes.ModuleName, govtypes.ModuleName,
		genutiltypes.ModuleName, feegrant.ModuleName,
		paramstypes.ModuleName, consensusparamtypes.ModuleName,
		// Phi modules before crisis; credentials after identity, disclosure after credentials.
		identitytypes.ModuleName, cointypes.ModuleName, institutionstypes.ModuleName,
		credentialstypes.ModuleName, disclosuretypes.ModuleName, votingtypes.ModuleName,
		// governance carries only the vote-route table.
		governancetypes.ModuleName,
		// crisis last: asserts genesis invariants after all state is ready.
		crisistypes.ModuleName,
	}
	app.ModuleManager.SetOrderInitGenesis(genesisModuleOrder...)
	app.ModuleManager.SetOrderExportGenesis(genesisModuleOrder...)

	// Register module invariants with x/crisis (Manager.RegisterInvariants is a no-op in v0.53); sorted order for a deterministic crisis report.
	invModuleNames := make([]string, 0, len(app.ModuleManager.Modules))
	for name := range app.ModuleManager.Modules {
		invModuleNames = append(invModuleNames, name)
	}
	sort.Strings(invModuleNames)
	for _, name := range invModuleNames {
		if hi, ok := app.ModuleManager.Modules[name].(module.HasInvariants); ok {
			hi.RegisterInvariants(app.CrisisKeeper)
		}
	}
	app.configurator = module.NewConfigurator(app.appCodec, app.MsgServiceRouter(), app.GRPCQueryRouter())
	if err := app.ModuleManager.RegisterServices(app.configurator); err != nil {
		panic(err)
	}

	autocliv1.RegisterQueryServer(app.GRPCQueryRouter(), runtimeservices.NewAutoCLIQueryService(app.ModuleManager.Modules))
	reflectionSvc, err := runtimeservices.NewReflectionService()
	if err != nil {
		panic(err)
	}
	reflectionv1.RegisterReflectionServiceServer(app.GRPCQueryRouter(), reflectionSvc)

	app.MountKVStores(keys)
	app.MountTransientStores(tkeys)

	app.SetInitChainer(app.InitChainer)
	app.SetPreBlocker(app.PreBlocker)
	app.SetBeginBlocker(app.BeginBlocker)
	app.SetEndBlocker(app.EndBlocker)
	app.setAnteHandler()

	if loadLatest {
		if err := app.LoadLatestVersion(); err != nil {
			panic(fmt.Errorf("error loading last version: %w", err))
		}
	}

	// Consensus crypto-verifier guard: a node on the fail-closed Disabled stub (built without -tags phicrypto_cgo) must never join consensus at any height.
	if enforceCrypto && !phicrypto.DefaultEnforces() {
		panic("phi-crypto verifier is Disabled: build the node with -tags phicrypto_cgo to run a node")
	}

	return app
}

func (app *App) setAnteHandler() {
	anteHandler, err := phiante.NewAnteHandler(phiante.HandlerOptions{
		AccountKeeper:   app.AccountKeeper,
		BankKeeper:      app.BankKeeper,
		FeegrantKeeper:  app.FeeGrantKeeper,
		SignModeHandler: app.txConfig.SignModeHandler(),
		CoinKeeper:      app.CoinKeeper,
		// WebAuthn slot bound to the phi-crypto port (Disabled until the cgo link); relying-party origin allow-list + rpId are read from governed x/identity params.
		Verifier:       phicrypto.Default(),
		WebAuthnParams: app.IdentityKeeper,
		// The gov-param guard reads the institution vault total to refuse arming a uphi deposit burn.
		VaultReader: app.InstitutionsKeeper,
		// The DID-lifecycle guard reads controller→DID status to reject suspended/revoked signers.
		IdentityStatus: app.IdentityKeeper,
		// The stepped-UV policy (which messages are sensitive) is a governed x/identity param.
		UVPolicy: app.IdentityKeeper,
	})
	if err != nil {
		panic(err)
	}
	app.SetAnteHandler(anteHandler)
}

// Name returns the application name.
func (app *App) Name() string { return app.BaseApp.Name() }

// PreBlocker runs the module manager pre-block hook.
func (app *App) PreBlocker(ctx sdk.Context, _ *abci.RequestFinalizeBlock) (*sdk.ResponsePreBlock, error) {
	return app.ModuleManager.PreBlock(ctx)
}

// BeginBlocker runs the module manager begin-block hook.
func (app *App) BeginBlocker(ctx sdk.Context) (sdk.BeginBlock, error) {
	return app.ModuleManager.BeginBlock(ctx)
}

// EndBlocker runs the module manager end-block hook.
func (app *App) EndBlocker(ctx sdk.Context) (sdk.EndBlock, error) {
	return app.ModuleManager.EndBlock(ctx)
}

// Configurator returns the app configurator.
func (app *App) Configurator() module.Configurator { return app.configurator }

// InitChainer performs the initial chain setup.
func (app *App) InitChainer(ctx sdk.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	var genesisState map[string]json.RawMessage
	if err := json.Unmarshal(req.AppStateBytes, &genesisState); err != nil {
		panic(err)
	}
	res, err := app.ModuleManager.InitGenesis(ctx, app.appCodec, genesisState)
	if err != nil {
		return res, err
	}
	if err := app.EnforceFiniteBlockMaxGas(ctx); err != nil {
		return res, err
	}
	// Return the (possibly capped) consensus params to CometBFT.
	cp := app.GetConsensusParams(ctx)
	res.ConsensusParams = &cp
	return res, nil
}

// EnforceFiniteBlockMaxGas bounds per-block compute.
func (app *App) EnforceFiniteBlockMaxGas(ctx sdk.Context) error {
	cp := app.GetConsensusParams(ctx)
	if cp.Block != nil && cp.Block.MaxGas <= 0 {
		cp.Block.MaxGas = DefaultBlockMaxGas
		return app.StoreConsensusParams(ctx, cp)
	}
	return nil
}

// LoadHeight loads a specific block height.
func (app *App) LoadHeight(height int64) error { return app.LoadVersion(height) }

// LegacyAmino returns the legacy Amino codec.
func (app *App) LegacyAmino() *codec.LegacyAmino { return app.legacyAmino }

// AppCodec returns the app codec.
func (app *App) AppCodec() codec.Codec { return app.appCodec }

// InterfaceRegistry returns the interface registry.
func (app *App) InterfaceRegistry() codectypes.InterfaceRegistry { return app.interfaceRegistry }

// TxConfig returns the tx config.
func (app *App) TxConfig() client.TxConfig { return app.txConfig }

// AutoCliOpts returns the autocli options.
func (app *App) AutoCliOpts() autocli.AppOptions {
	modules := make(map[string]appmodule.AppModule, 0)
	for _, m := range app.ModuleManager.Modules {
		if moduleWithName, ok := m.(module.HasName); ok {
			if appModule, ok := moduleWithName.(appmodule.AppModule); ok {
				modules[moduleWithName.Name()] = appModule
			}
		}
	}
	return autocli.AppOptions{
		Modules:               modules,
		ModuleOptions:         runtimeservices.ExtractAutoCLIOptions(app.ModuleManager.Modules),
		AddressCodec:          authcodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix()),
		ValidatorAddressCodec: authcodec.NewBech32Codec(sdk.GetConfig().GetBech32ValidatorAddrPrefix()),
		ConsensusAddressCodec: authcodec.NewBech32Codec(sdk.GetConfig().GetBech32ConsensusAddrPrefix()),
	}
}

// DefaultGenesis returns the default genesis from all modules.
func (app *App) DefaultGenesis() map[string]json.RawMessage {
	return app.BasicModuleManager.DefaultGenesis(app.appCodec)
}

// GetKey returns a store key (for testing).
func (app *App) GetKey(storeKey string) *storetypes.KVStoreKey { return app.keys[storeKey] }

// GetStoreKeys returns all store keys.
func (app *App) GetStoreKeys() []storetypes.StoreKey {
	out := make([]storetypes.StoreKey, 0, len(app.keys))
	for _, k := range app.keys {
		out = append(out, k)
	}
	return out
}

// GetSubspace returns a module's param subspace (for legacy compatibility).
func (app *App) GetSubspace(moduleName string) paramstypes.Subspace {
	subspace, _ := app.ParamsKeeper.GetSubspace(moduleName)
	return subspace
}

// SimulationManager returns nil; Phi has no fuzz simulator.
func (app *App) SimulationManager() *module.SimulationManager { return nil }

// RegisterAPIRoutes registers the API routes (REST/LCD + gRPC-gateway).
func (app *App) RegisterAPIRoutes(apiSvr *api.Server, apiConfig config.APIConfig) {
	clientCtx := apiSvr.ClientCtx
	authtx.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	cmtservice.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	nodeservice.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	app.BasicModuleManager.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	if err := server.RegisterSwaggerAPI(apiSvr.ClientCtx, apiSvr.Router, apiConfig.Swagger); err != nil {
		panic(err)
	}
}

// RegisterTxService registers the tx service.
func (app *App) RegisterTxService(clientCtx client.Context) {
	authtx.RegisterTxService(app.BaseApp.GRPCQueryRouter(), clientCtx, app.BaseApp.Simulate, app.interfaceRegistry)
}

// RegisterTendermintService registers the Tendermint service.
func (app *App) RegisterTendermintService(clientCtx client.Context) {
	cmtApp := server.NewCometABCIWrapper(app)
	cmtservice.RegisterTendermintService(clientCtx, app.BaseApp.GRPCQueryRouter(), app.interfaceRegistry, cmtApp.Query)
}

// RegisterNodeService registers the node service.
func (app *App) RegisterNodeService(clientCtx client.Context, cfg config.Config) {
	nodeservice.RegisterNodeService(clientCtx, app.GRPCQueryRouter(), cfg)
}

// GetMaccPerms returns a copy of the module account permissions.
func GetMaccPerms() map[string][]string {
	dup := make(map[string][]string)
	for k, v := range maccPerms {
		dup[k] = v
	}
	return dup
}

// BlockedAddresses returns the blocked addresses (module accounts), allowing gov to receive funds.
func (app *App) BlockedAddresses() map[string]bool {
	modAccAddrs := make(map[string]bool)
	for acc := range GetMaccPerms() {
		modAccAddrs[authtypes.NewModuleAddress(acc).String()] = true
	}
	delete(modAccAddrs, authtypes.NewModuleAddress(govtypes.ModuleName).String())
	return modAccAddrs
}

func initParamsKeeper(appCodec codec.BinaryCodec, legacyAmino *codec.LegacyAmino, key, tkey storetypes.StoreKey) paramskeeper.Keeper {
	pk := paramskeeper.NewKeeper(appCodec, legacyAmino, key, tkey)
	pk.Subspace(authtypes.ModuleName)
	pk.Subspace(banktypes.ModuleName)
	pk.Subspace(stakingtypes.ModuleName)
	pk.Subspace(distrtypes.ModuleName)
	pk.Subspace(slashingtypes.ModuleName)
	pk.Subspace(govtypes.ModuleName)
	pk.Subspace(crisistypes.ModuleName)
	return pk
}

type govProposalAdapter struct{ gk *govkeeper.Keeper }

// Proposal returns the proposal with the given id (or an error if absent).
func (a govProposalAdapter) Proposal(ctx context.Context, id uint64) (govv1.Proposal, error) {
	return a.gk.Proposals.Get(ctx, id)
}
