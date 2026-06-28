// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	"github.com/Port-PHI/phi-chain/app"
	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	institutionstypes "github.com/Port-PHI/phi-chain/x/institutions/types"
)

const (
	flagLicense      = "license"
	flagVaultAccount = "vault-account"
	flagVaultAPI     = "vault-api"
	flagBond         = "bond"
	flagVaultBalance = "vault-balance"
	flagAttested     = "attested-reserve"
	flagBackSupply   = "back-genesis-supply"
	flagAsOperator   = "operator"
)

// AddInstitutionCmd returns the `phid genesis add-institution` command.
//
// Why it is needed: since every PHI must be backed by an institution, a genesis with an initial
// uphi allocation (e.g. for staking) must include a "bootstrap operator institution" with an
// equivalent vault_balance so that the global solvency invariant
// (`supply_uphi × phi_to_toman = Σ vault_balance × UphiPerPhi`) holds at genesis; otherwise the
// node panics at startup. This command adds that institution to genesis.
func AddInstitutionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-institution [id] [admin_address]",
		Short: "Add a financial institution to genesis (bootstrap vault backing)",
		Long: `Adds a financial institution to the initial state of the institutions module in genesis.json.

For bootstrapping, --back-genesis-supply computes vault_balance automatically from the total
genesis uphi supply so the global solvency invariant holds exactly (run this command after
add-genesis-account). With --operator, this admin is set as the institutions registry operator.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)
			serverCtx := server.GetServerContextFromCmd(cmd)
			config := serverCtx.Config
			config.SetRoot(clientCtx.HomeDir)
			cdc := clientCtx.Codec

			id := args[0]
			adminStr := args[1]
			if id == "" {
				return fmt.Errorf("institution id cannot be empty")
			}
			if _, err := sdk.AccAddressFromBech32(adminStr); err != nil {
				return fmt.Errorf("invalid admin address: %w", err)
			}

			license, _ := cmd.Flags().GetString(flagLicense)
			vaultAccount, _ := cmd.Flags().GetString(flagVaultAccount)
			vaultAPI, _ := cmd.Flags().GetString(flagVaultAPI)
			bond, _ := cmd.Flags().GetString(flagBond)
			vaultBalance, _ := cmd.Flags().GetString(flagVaultBalance)
			attested, _ := cmd.Flags().GetString(flagAttested)
			backSupply, _ := cmd.Flags().GetBool(flagBackSupply)
			asOperator, _ := cmd.Flags().GetBool(flagAsOperator)
			if vaultAccount == "" {
				vaultAccount = "vault-" + id
			}

			genFile := config.GenesisFile()
			appGenesis, err := genutiltypes.AppGenesisFromFile(genFile)
			if err != nil {
				return err
			}
			var appState map[string]json.RawMessage
			if err := json.Unmarshal(appGenesis.AppState, &appState); err != nil {
				return fmt.Errorf("failed to unmarshal genesis app_state: %w", err)
			}

			var instGen institutionstypes.GenesisState
			cdc.MustUnmarshalJSON(appState[institutionstypes.ModuleName], &instGen)

			phiToToman := instGen.Params.PhiToToman
			if phiToToman == 0 {
				phiToToman = institutionstypes.DefaultPhiToToman
			}

			// Automatically compute the backing to cover the total genesis uphi supply.
			if backSupply {
				var bankGen banktypes.GenesisState
				cdc.MustUnmarshalJSON(appState[banktypes.ModuleName], &bankGen)
				supply := math.ZeroInt()
				for _, b := range bankGen.Balances {
					supply = supply.Add(b.Coins.AmountOf(cointypes.Denom))
				}
				num := supply.Mul(math.NewIntFromUint64(phiToToman))
				den := math.NewIntFromUint64(cointypes.UphiPerPhi)
				if !num.Mod(den).IsZero() {
					return fmt.Errorf("genesis uphi supply %s is not divisible — vault_balance would be non-integral; adjust allocations so solvency holds exactly", supply)
				}
				vaultBalance = num.Quo(den).String()
			}
			if vaultBalance == "" {
				vaultBalance = "0"
			}
			if v, ok := math.NewIntFromString(vaultBalance); !ok || v.IsNegative() {
				return fmt.Errorf("invalid vault-balance %q", vaultBalance)
			}
			if attested == "" {
				attested = vaultBalance
			}

			for _, in := range instGen.Institutions {
				if in.Id == id {
					return fmt.Errorf("institution %q is already in genesis", id)
				}
			}

			instGen.Institutions = append(instGen.Institutions, institutionstypes.Institution{
				Id:              id,
				License:         license,
				Admin:           adminStr,
				VaultAccount:    vaultAccount,
				VaultApi:        vaultAPI,
				Bond:            bond,
				Status:          institutionstypes.INSTITUTION_STATUS_HEALTHY,
				VaultBalance:    vaultBalance,
				AttestedReserve: attested,
				PausedMint:      false,
				// Set the type explicitly. Leaving it UNSPECIFIED (0) would fail genesis
				// validation and otherwise let the bootstrap institution skip the fx provenance rules.
				InstitutionType: institutionstypes.INSTITUTION_TYPE_FINANCIAL,
			})
			instGen.Params.PhiToToman = phiToToman
			if asOperator {
				instGen.Params.Operator = adminStr
			}
			if err := instGen.Validate(); err != nil {
				return fmt.Errorf("resulting institutions genesis is invalid: %w", err)
			}

			appState[institutionstypes.ModuleName] = cdc.MustMarshalJSON(&instGen)
			appGenesis.AppState, err = json.Marshal(appState)
			if err != nil {
				return fmt.Errorf("failed to marshal genesis app_state: %w", err)
			}
			if err := genutil.ExportGenesisFile(appGenesis, genFile); err != nil {
				return err
			}
			cmd.Printf("institution %q added to genesis (vault_balance=%s toman, attested_reserve=%s).\n", id, vaultBalance, attested)
			return nil
		},
	}

	cmd.Flags().String(flags.FlagHome, app.DefaultNodeHome, "The application home directory")
	cmd.Flags().String(flagLicense, "GENESIS-BOOTSTRAP", "central bank license reference (hash/id)")
	cmd.Flags().String(flagVaultAccount, "", "vault account (default: vault-<id>)")
	cmd.Flags().String(flagVaultAPI, "", "live vault API endpoint (phi-bridge)")
	cmd.Flags().String(flagBond, "0", "performance bond (uphi)")
	cmd.Flags().String(flagVaultBalance, "", "vault balance (toman) — explicit")
	cmd.Flags().String(flagAttested, "", "attested reserve (toman; default = vault-balance)")
	cmd.Flags().Bool(flagBackSupply, false, "automatically compute vault-balance to cover the total genesis uphi supply (solvency invariant)")
	cmd.Flags().Bool(flagAsOperator, false, "set this admin as the institutions registry operator")
	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
