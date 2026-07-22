// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"encoding/json"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/app"
	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
)

var genesisChainTime = time.Unix(1_700_000_000, 0).UTC()

type roundTripChain struct {
	app     *app.App
	genesis map[string]json.RawMessage
}

func startChain(t *testing.T, genesis map[string]json.RawMessage) *roundTripChain {
	t.Helper()

	a := app.NewApp(log.NewNopLogger(), dbm.NewMemDB(), nil, true,
		simtestutil.NewAppOptionsWithFlagHome(t.TempDir()), false)

	stateBytes, err := json.Marshal(genesis)
	require.NoError(t, err)

	_, err = a.InitChain(&abci.RequestInitChain{
		Time:            genesisChainTime,
		ConsensusParams: simtestutil.DefaultConsensusParams,
		AppStateBytes:   stateBytes,
	})
	require.NoError(t, err, "InitChain must accept the genesis")

	_, err = a.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: a.LastBlockHeight() + 1,
		Time:   genesisChainTime,
	})
	require.NoError(t, err, "the first block must not abort")
	_, err = a.Commit()
	require.NoError(t, err)

	return &roundTripChain{app: a, genesis: genesis}
}

func (c *roundTripChain) advanceBlock(t *testing.T) {
	t.Helper()
	_, err := c.app.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: c.app.LastBlockHeight() + 1,
		Time:   genesisChainTime,
	})
	require.NoError(t, err, "the block over the seeded state must not abort")
	_, err = c.app.Commit()
	require.NoError(t, err)
}

// export runs the real export entry point and returns the app state as a per-module document.
func (c *roundTripChain) export(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	exported, err := c.app.ExportAppStateAndValidators(false, nil, nil)
	require.NoError(t, err, "ExportAppStateAndValidators must not fail")

	var out map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(exported.AppState, &out))
	return out
}

func (c *roundTripChain) ctx() sdk.Context {
	return c.app.NewUncachedContext(true, cmtproto.Header{
		Height: c.app.LastBlockHeight(), Time: genesisChainTime,
	})
}

type genesisValidator struct {
	valSet  *cmttypes.ValidatorSet
	account authtypes.GenesisAccount
	addr    sdk.AccAddress
	balance math.Int
}

func newGenesisValidator(t *testing.T) genesisValidator {
	t.Helper()
	pub := ed25519.GenPrivKeyFromSecret([]byte("roundtrip-founder")).PubKey()
	cmtPub, err := cryptocodec.ToCmtPubKeyInterface(pub)
	require.NoError(t, err)

	addr := sdk.AccAddress(pub.Address())
	return genesisValidator{
		valSet:  cmttypes.NewValidatorSet([]*cmttypes.Validator{cmttypes.NewValidator(cmtPub, 1)}),
		account: &authtypes.BaseAccount{Address: addr.String()},
		addr:    addr,
		balance: math.NewIntFromUint64(cointypes.UphiPerPhi).MulRaw(1_000),
	}
}

func baseGenesis(t *testing.T, gv genesisValidator) map[string]json.RawMessage {
	t.Helper()
	scratch := newTestApp(t)
	genesis := scratch.DefaultGenesis()

	withVals, err := simtestutil.GenesisStateWithValSet(
		scratch.AppCodec(), genesis, gv.valSet,
		[]authtypes.GenesisAccount{gv.account},
		banktypes.Balance{Address: gv.addr.String(), Coins: cointypes.CoinsOf(gv.balance)},
	)
	require.NoError(t, err)
	return withVals
}

// TestNet_AppGenesisRoundTrips is the net itself: a chain carrying rich state exports, a fresh chain starts from that export, and both the export AND the live state must come back identical.
func TestNet_AppGenesisRoundTrips(t *testing.T) {
	gv := newGenesisValidator(t)

	first := startChain(t, seedRichGenesis(t, baseGenesis(t, gv), gv))
	seedEveryKeyspace(t, first, gv)
	first.advanceBlock(t)

	before := first.dumpAll()
	requireEveryKeyspaceSeeded(t, before)

	exportedOnce := first.export(t)
	second := startChain(t, exportedOnce)
	exportedTwice := second.export(t)

	for module := range exportedOnce {
		require.JSONEq(t, string(exportedOnce[module]), string(exportedTwice[module]),
			"module %q did not survive export → import → export", module)
	}
	require.Equal(t, len(exportedOnce), len(exportedTwice), "a module vanished from the export")

	requireEveryKeyspaceRoundTripped(t, before, second.dumpAll())
}
