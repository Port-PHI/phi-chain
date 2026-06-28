// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"

	clientv2helpers "cosmossdk.io/client/v2/helpers"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"

	"github.com/Port-PHI/phi-chain/app"
	"github.com/Port-PHI/phi-chain/cmd/phid/cmd"
)

func main() {
	cmd.PrintCommandBanner() // φ logo shown on node startup and `phid version`
	rootCmd := cmd.NewRootCmd()
	if err := svrcmd.Execute(rootCmd, clientv2helpers.EnvPrefix, app.DefaultNodeHome); err != nil {
		fmt.Fprintln(rootCmd.OutOrStderr(), err)
		os.Exit(1)
	}
}
