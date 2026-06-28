// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

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
	// AppName is the application name.
	AppName = "phi"
	// AccountAddressPrefix is the account address prefix.
	AccountAddressPrefix = "phi"
)

var (
	// DefaultNodeHome is the default node home directory.
	DefaultNodeHome string

	// maccPerms holds the module account permissions.
	maccPerms = map[string][]string{
		authtypes.FeeCollectorName:     nil,
		distrtypes.ModuleName:          nil,
		stakingtypes.BondedPoolName:    {authtypes.Burner, authtypes.Staking},
		stakingtypes.NotBondedPoolName: {authtypes.Burner, authtypes.Staking},
		govtypes.ModuleName:            {authtypes.Burner},
		// Institutions mint/burn only against their own vault (multi-institution model).
		institutionstypes.ModuleName: {authtypes.Minter, authtypes.Burner},
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

	// Standard keepers.
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

	// Phi-specific keepers.
	IdentityKeeper     identitykeeper.Keeper
	CoinKeeper         coinkeeper.Keeper
	InstitutionsKeeper institutionskeeper.Keeper
	CredentialsKeeper  credentialskeeper.Keeper
	DisclosureKeeper   disclosurekeeper.Keeper
	VotingKeeper       votingkeeper.Keeper

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

// NewApp creates and initializes an App instance. enforceCrypto must be true for a consensus node
// (the start / prune / snapshot path): it refuses to construct on the fail-closed Disabled verifier
// (a build without -tags phicrypto_cgo) at any height, so a tagless binary cannot run a node and fork
// from cgo-built validators. Offline/export/gentx tooling and tests pass false.
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

	// x/slashing drives validator slashing. Wrap the staking keeper it calls so the WHOLE slash — the
	// validator-direct burn plus the unbonding-delegation/redelegation burns the SDK performs on a
	// past-height infraction — is measured and re-minted to the penalty destination, keeping uphi supply
	// (and the solvency invariant) constant. The penalty escrow is the institutions keeper, constructed
	// below; a pointer to that field late-binds it (no slash runs during wiring).
	app.SlashingKeeper = slashingkeeper.NewKeeper(appCodec, legacyAmino, runtime.NewKVStoreService(keys[slashingtypes.StoreKey]),
		newSlashCompensator(app.StakingKeeper, app.BankKeeper, &app.InstitutionsKeeper), govModAddr)

	invCheckPeriod := cast.ToUint(appOpts.Get(server.FlagInvCheckPeriod))
	app.CrisisKeeper = crisiskeeper.NewKeeper(appCodec, runtime.NewKVStoreService(keys[crisistypes.StoreKey]), invCheckPeriod, app.BankKeeper, authtypes.FeeCollectorName, govModAddr, app.AccountKeeper.AddressCodec())

	app.FeeGrantKeeper = feegrantkeeper.NewKeeper(appCodec, runtime.NewKVStoreService(keys[feegrant.StoreKey]), app.AccountKeeper)

	// x/evidence turns CometBFT-reported equivocation (double-sign) into a slash + jail + tombstone. Its
	// BeginBlocker reads misbehavior from comet (ProvideCometInfoService) and calls the slashing keeper,
	// whose staking keeper is the slash-compensation wrapper above — so the resulting equivocation burn is
	// supply-compensated like any other slash and the solvency invariant is preserved. No custom handler
	// router is set: CometBFT equivocation is handled directly in the keeper's BeginBlocker.
	evidenceKeeper := evidencekeeper.NewKeeper(appCodec, runtime.NewKVStoreService(keys[evidencetypes.StoreKey]),
		app.StakingKeeper, app.SlashingKeeper, app.AccountKeeper.AddressCodec(), runtime.ProvideCometInfoService())
	app.EvidenceKeeper = *evidenceKeeper

	// Phi-specific keepers (built before staking hooks, since the validator hook depends on IdentityKeeper).
	app.IdentityKeeper = identitykeeper.NewKeeper(appCodec, keys[identitytypes.StoreKey], govModAddr, phicrypto.Default())
	app.CoinKeeper = coinkeeper.NewKeeper(appCodec, keys[cointypes.StoreKey], govModAddr, app.BankKeeper)
	app.InstitutionsKeeper = institutionskeeper.NewKeeper(appCodec, keys[institutionstypes.StoreKey], govModAddr, app.BankKeeper, app.IdentityKeeper, app.CoinKeeper, phicrypto.Default())
	// credentials: BBS+/signature verification goes through phicrypto.Default() (no build tag = Disabled, fail-safe);
	// real verification is enabled when the node is built with the cgo verifier (-tags phicrypto_cgo).
	app.CredentialsKeeper = credentialskeeper.NewKeeper(appCodec, keys[credentialstypes.StoreKey], govModAddr, app.IdentityKeeper, phicrypto.Default())
	// disclosure: verify-only; verifies a BBS+ selective-disclosure proof against the credentials anchor and the
	// issuer's BBS key via phicrypto.Default() (Disabled until the cgo link). No per-disclosure state.
	app.DisclosureKeeper = disclosurekeeper.NewKeeper(appCodec, keys[disclosuretypes.StoreKey], govModAddr, app.CredentialsKeeper, phicrypto.Default())
	// voting: anonymous public voting; eligibility is verified with a BBS+ proof against the template issuer key and
	// the nullifier (per-election) is deduplicated (phicrypto.Default() -> Disabled until the cgo link).
	app.VotingKeeper = votingkeeper.NewKeeper(appCodec, keys[votingtypes.StoreKey], govModAddr, app.CredentialsKeeper, phicrypto.Default(), votingkeeper.VotingSoundnessEnforced)

	// Staking hooks: distr + slashing + the Phi validator rule (unique DID + minimum 1000 PHI self-stake).
	// Every runtime validator must be a unique verified human; the genesis founder set is exempt.
	minSelfStake := math.NewInt(1000).Mul(math.NewIntFromUint64(cointypes.UphiPerPhi))
	app.StakingKeeper.SetHooks(stakingtypes.NewMultiStakingHooks(
		app.DistrKeeper.Hooks(),
		app.SlashingKeeper.Hooks(),
		// The Phi validator rule (unique DID + minimum self-stake). Supply conservation across slashing
		// is handled by the slash-compensation wrapper on the slashing keeper's staking keeper (above),
		// not by these hooks.
		app.IdentityKeeper.NewValidatorHooks(app.StakingKeeper, minSelfStake),
	))

	// gov
	govRouter := govv1beta1.NewRouter()
	govRouter.AddRoute(govtypes.RouterKey, govv1beta1.ProposalHandler)
	govConfig := govtypes.DefaultConfig()
	// Custom one-human-one-vote (public) / validator-weighted (technical) tally, injected through the official
	// Cosmos hook without overriding the EndBlocker (governance.NewPhiTallyFn over IdentityKeeper).
	govKeeper := govkeeper.NewKeeper(appCodec, runtime.NewKVStoreService(keys[govtypes.StoreKey]), app.AccountKeeper, app.BankKeeper, app.StakingKeeper, app.DistrKeeper, app.MsgServiceRouter(), govConfig, govModAddr,
		govkeeper.WithCustomCalculateVoteResultsAndVotingPowerFn(governance.NewPhiTallyFn(app.IdentityKeeper)))
	govKeeper.SetLegacyRouter(govRouter)
	app.GovKeeper = *govKeeper.SetHooks(govtypes.NewMultiGovHooks())

	// Inject the gov keeper into the institutions keeper (built earlier): fx onboarding finalization
	// outside the bootstrap phase requires a PASSED public proposal. Must run before the institutions
	// AppModule copies the keeper by value below.
	app.InstitutionsKeeper.SetGovKeeper(govProposalAdapter{gk: &app.GovKeeper})

	// Modules.
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
		// Phi modules.
		identity.NewAppModule(appCodec, app.IdentityKeeper),
		coin.NewAppModule(appCodec, app.CoinKeeper),
		institutions.NewAppModule(appCodec, app.InstitutionsKeeper),
		credentials.NewAppModule(appCodec, app.CredentialsKeeper),
		disclosure.NewAppModule(appCodec, app.DisclosureKeeper),
		voting.NewAppModule(appCodec, app.VotingKeeper),
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
		// evidence runs before slashing/staking so a CometBFT-reported equivocation is slashed, jailed and
		// tombstoned this block — before staking processes the validator set — and the slash flows through
		// the compensation wrapper, keeping uphi supply (and the solvency invariant) intact.
		distrtypes.ModuleName, evidencetypes.ModuleName, slashingtypes.ModuleName, stakingtypes.ModuleName, genutiltypes.ModuleName,
		// coin: BeginBlock prunes stale daily micro-exemption quota keys; no ordering dependency.
		cointypes.ModuleName,
	)
	app.ModuleManager.SetOrderEndBlockers(
		crisistypes.ModuleName, govtypes.ModuleName, stakingtypes.ModuleName, genutiltypes.ModuleName, feegrant.ModuleName,
		// institutions asserts the solvency invariants in EndBlock (defense-in-depth): slashing and any
		// other out-of-band supply change happens in begin/end-block, which the keeper write-path
		// assertSolvency never sees. Runs after staking so any slash this block is already compensated.
		institutionstypes.ModuleName,
	)

	genesisModuleOrder := []string{
		authtypes.ModuleName, banktypes.ModuleName,
		distrtypes.ModuleName, stakingtypes.ModuleName, slashingtypes.ModuleName, evidencetypes.ModuleName, govtypes.ModuleName,
		genutiltypes.ModuleName, feegrant.ModuleName,
		paramstypes.ModuleName, consensusparamtypes.ModuleName,
		// Phi modules are initialized before crisis so the vault state is ready.
		// credentials comes after identity (it depends on it); disclosure comes after credentials.
		identitytypes.ModuleName, cointypes.ModuleName, institutionstypes.ModuleName,
		credentialstypes.ModuleName, disclosuretypes.ModuleName, votingtypes.ModuleName,
		// crisis must be last: it asserts genesis invariants after all state is ready.
		crisistypes.ModuleName,
	}
	app.ModuleManager.SetOrderInitGenesis(genesisModuleOrder...)
	app.ModuleManager.SetOrderExportGenesis(genesisModuleOrder...)

	// Register module invariants with x/crisis. module.Manager.RegisterInvariants is a no-op in
	// SDK v0.53, so iterate the modules and register each one that has invariants. Without this,
	// x/crisis has zero routes and the institutions solvency invariants are never enforced at runtime.
	for _, m := range app.ModuleManager.Modules {
		if hi, ok := m.(module.HasInvariants); ok {
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

	// Mount stores and BaseApp.
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

	// Consensus-critical crypto-verifier guard. A node whose phi-crypto verifier is the
	// fail-closed Disabled stub (built WITHOUT -tags phicrypto_cgo) rejects every crypto-dependent
	// message, so it must never participate in consensus at ANY height — including genesis (height 0) —
	// or it forks from cgo-built validators. Enforcement is keyed on the explicit enforceCrypto flag,
	// set only by the node start/prune/snapshot path, NOT on block height: offline/export/gentx and
	// tests pass enforceCrypto=false and stay usable with the default (tagless) build. Production node
	// builds MUST set -tags phicrypto_cgo.
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
		// WebAuthn slot bound to the phi-crypto port (Disabled until the cgo link); relying-party
		// origin allow-list + rpId are read from governed x/identity params.
		Verifier:       phicrypto.Default(),
		WebAuthnParams: app.IdentityKeeper,
		// The gov-param guard reads the institution vault total to refuse arming a uphi deposit burn.
		VaultReader: app.InstitutionsKeeper,
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
	// Return the (possibly capped) consensus params to CometBFT. StoreConsensusParams only updates
	// the app-side param store; CometBFT keeps the genesis consensus_params for block 1 unless we surface
	// them here, so without this the block-gas cap from EnforceFiniteBlockMaxGas would not bind the first
	// block at the CometBFT layer. Setting res.ConsensusParams makes CometBFT adopt the capped params.
	cp := app.GetConsensusParams(ctx)
	res.ConsensusParams = &cp
	return res, nil
}

// EnforceFiniteBlockMaxGas bounds per-block compute. CometBFT's default block MaxGas is -1
// (unlimited); with a fixed per-message fee that does not price metered gas, an unlimited block lets a
// tx buy unbounded validator compute. This caps an unlimited genesis value to a finite default so a
// maximally expensive single-message tx (gas > MaxGas) is rejected by the block gas meter; an
// explicitly-set finite value is respected. It updates the app-side consensus-param store; the
// InitChainer then returns the capped params in ResponseInitChain so CometBFT adopts them too.
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

// initParamsKeeper builds the legacy subspaces.
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

// govProposalAdapter adapts the x/gov keeper's Proposals collection to the institutions
// types.GovKeeper port (used by fx onboarding's passed-proposal gate).
type govProposalAdapter struct{ gk *govkeeper.Keeper }

// Proposal returns the proposal with the given id (or an error if absent).
func (a govProposalAdapter) Proposal(ctx context.Context, id uint64) (govv1.Proposal, error) {
	return a.gk.Proposals.Get(ctx, id)
}
