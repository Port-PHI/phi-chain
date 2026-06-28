// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"io"

	cmtcfg "github.com/cometbft/cometbft/config"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"cosmossdk.io/log"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/debug"
	"github.com/cosmos/cosmos-sdk/client/keys"
	"github.com/cosmos/cosmos-sdk/client/pruning"
	"github.com/cosmos/cosmos-sdk/client/rpc"
	"github.com/cosmos/cosmos-sdk/client/snapshot"
	"github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authcmd "github.com/cosmos/cosmos-sdk/x/auth/client/cli"
	"github.com/cosmos/cosmos-sdk/x/crisis"
	genutilcli "github.com/cosmos/cosmos-sdk/x/genutil/client/cli"

	"github.com/Port-PHI/phi-chain/app"
)

// initCometBFTConfig returns the default CometBFT config.
// Important: the database backend is set to **pebbledb** (not the default goleveldb).
// Under Go 1.25+ goleveldb has a read-after-write issue in this environment (reading IAVL
// versions fails and state queries break); pebbledb (cockroachdb/pebble, pure Go) is stable.
// The binary must be built with `-tags pebbledb` (the Makefile does this).
func initCometBFTConfig() *cmtcfg.Config {
	cfg := cmtcfg.DefaultConfig()
	cfg.DBBackend = "pebbledb"
	return cfg
}

// initAppConfig defines the default app.toml template and values.
func initAppConfig() (string, interface{}) {
	srvCfg := serverconfig.DefaultConfig()
	// The Phi chain uses a fixed per-message fee (not a gas price); minimum gas price is zero.
	srvCfg.MinGasPrices = "0uphi"
	return serverconfig.DefaultConfigTemplate, *srvCfg
}

func initRootCmd(rootCmd *cobra.Command, txConfig client.TxConfig, basicManager module.BasicManager) {
	cfg := sdk.GetConfig()
	cfg.Seal()

	rootCmd.AddCommand(
		genutilcli.InitCmd(basicManager, app.DefaultNodeHome),
		debug.Cmd(),
		pruning.Cmd(newApp, app.DefaultNodeHome),
		snapshot.Cmd(newApp),
	)

	server.AddCommandsWithStartCmdOptions(rootCmd, app.DefaultNodeHome, newApp, appExport, server.StartCmdOptions{
		AddFlags: func(startCmd *cobra.Command) {
			crisis.AddModuleInitFlags(startCmd)
		},
	})

	rootCmd.AddCommand(
		server.StatusCommand(),
		genesisCommand(txConfig, basicManager, AddInstitutionCmd()),
		queryCommand(),
		txCommand(),
		keys.Commands(),
	)
}

// genesisCommand returns the `phid genesis` command (init, gentx, collect-gentxs, ...).
func genesisCommand(txConfig client.TxConfig, basicManager module.BasicManager, cmds ...*cobra.Command) *cobra.Command {
	cmd := genutilcli.Commands(txConfig, basicManager, app.DefaultNodeHome)
	for _, c := range cmds {
		cmd.AddCommand(c)
	}
	return cmd
}

func queryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "query",
		Aliases:                    []string{"q"},
		Short:                      "Querying subcommands",
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(
		rpc.WaitTxCmd(),
		server.QueryBlockCmd(),
		authcmd.QueryTxsByEventsCmd(),
		server.QueryBlocksCmd(),
		authcmd.QueryTxCmd(),
		server.QueryBlockResultsCmd(),
	)
	return cmd
}

func txCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "tx",
		Short:                      "Transactions subcommands",
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(
		authcmd.GetSignCommand(),
		authcmd.GetSignBatchCommand(),
		authcmd.GetMultiSignCommand(),
		authcmd.GetMultiSignBatchCmd(),
		authcmd.GetValidateSignaturesCommand(),
		authcmd.GetBroadcastCommand(),
		authcmd.GetEncodeCommand(),
		authcmd.GetDecodeCommand(),
		authcmd.GetSimulateCmd(),
	)
	return cmd
}

// newApp builds an App instance for execution (the node start / pruning / snapshot path). It passes
// enforceCrypto=true so NewApp refuses to construct a node on the fail-closed Disabled verifier (a
// build without -tags phicrypto_cgo) at any height — including genesis — the single consensus crypto
// guard. Tests and genesis/export tooling call app.NewApp with enforceCrypto=false and are
// unaffected; production node builds MUST set -tags phicrypto_cgo.
func newApp(logger log.Logger, db dbm.DB, traceStore io.Writer, appOpts servertypes.AppOptions) servertypes.Application {
	baseappOptions := server.DefaultBaseappOptions(appOpts)
	return app.NewApp(logger, db, traceStore, true, appOpts, true, baseappOptions...)
}

// appExport exports the chain state for building a genesis file.
func appExport(
	logger log.Logger,
	db dbm.DB,
	traceStore io.Writer,
	height int64,
	forZeroHeight bool,
	jailAllowedAddrs []string,
	appOpts servertypes.AppOptions,
	modulesToExport []string,
) (servertypes.ExportedApp, error) {
	viperAppOpts, ok := appOpts.(*viper.Viper)
	if !ok {
		return servertypes.ExportedApp{}, errors.New("appOpts is not viper.Viper")
	}
	viperAppOpts.Set(server.FlagInvCheckPeriod, 1)
	appOpts = viperAppOpts

	var phiApp *app.App
	if height != -1 {
		phiApp = app.NewApp(logger, db, traceStore, false, appOpts, false)
		if err := phiApp.LoadHeight(height); err != nil {
			return servertypes.ExportedApp{}, err
		}
	} else {
		phiApp = app.NewApp(logger, db, traceStore, true, appOpts, false)
	}
	return phiApp.ExportAppStateAndValidators(forZeroHeight, jailAllowedAddrs, modulesToExport)
}
