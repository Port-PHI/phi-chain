// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/require"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
)

type recordingBank struct {
	burned map[string]math.Int
	sends  []recordedSend
}

type recordedSend struct {
	from, to string
	amt      sdk.Coins
}

func newRecordingBank() *recordingBank {
	return &recordingBank{burned: map[string]math.Int{}}
}

func (b *recordingBank) BurnCoins(_ context.Context, name string, amt sdk.Coins) error {
	cur, ok := b.burned[name]
	if !ok {
		cur = math.ZeroInt()
	}
	b.burned[name] = cur.Add(amt.AmountOf(cointypes.Denom))
	return nil
}

func (b *recordingBank) SendCoinsFromModuleToModule(_ context.Context, from, to string, amt sdk.Coins) error {
	b.sends = append(b.sends, recordedSend{from: from, to: to, amt: amt})
	return nil
}

func (b *recordingBank) GetAllBalances(context.Context, sdk.AccAddress) sdk.Coins { return nil }
func (b *recordingBank) GetBalance(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewCoin(denom, math.ZeroInt())
}
func (b *recordingBank) LockedCoins(context.Context, sdk.AccAddress) sdk.Coins    { return nil }
func (b *recordingBank) SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins { return nil }
func (b *recordingBank) SendCoinsFromModuleToAccount(context.Context, string, sdk.AccAddress, sdk.Coins) error {
	return nil
}
func (b *recordingBank) SendCoinsFromAccountToModule(context.Context, sdk.AccAddress, string, sdk.Coins) error {
	return nil
}

type fixedVaults struct{ total math.Int }

func (v fixedVaults) SumVaultBalance(sdk.Context) math.Int { return v.total }

func testCtx() sdk.Context {
	return sdk.NewContext(nil, cmtproto.Header{}, false, log.NewNopLogger())
}

// TestGovBurnGuard_NeutralizesBurnWhileSolvent asserts the execution-time guard: while any vault backs uphi, a gov deposit burn is redirected to the fee collector (supply-neutral) instead of destroyed; once all vaults are empty, the burn proceeds normally.
func TestGovBurnGuard_NeutralizesBurnWhileSolvent(t *testing.T) {
	ctx := testCtx()
	burn := cointypes.CoinsOf(math.NewInt(1_000))

	bank := newRecordingBank()
	guard := newGovBurnGuard(bank, fixedVaults{total: math.NewInt(1)})
	require.NoError(t, guard.BurnCoins(ctx, govtypes.ModuleName, burn))
	require.True(t, guard.burnedTotal(bank).IsZero(), "no uphi may be destroyed while a vault is non-zero")
	require.Len(t, bank.sends, 1, "the deposit must be redirected exactly once")
	require.Equal(t, govtypes.ModuleName, bank.sends[0].from)
	require.Equal(t, authtypes.FeeCollectorName, bank.sends[0].to, "redirect target is the fee collector")
	require.Equal(t, burn, bank.sends[0].amt)

	bank2 := newRecordingBank()
	guard2 := newGovBurnGuard(bank2, fixedVaults{total: math.ZeroInt()})
	require.NoError(t, guard2.BurnCoins(ctx, govtypes.ModuleName, burn))
	require.Equal(t, math.NewInt(1_000), bank2.burned[govtypes.ModuleName], "with empty vaults the burn is real")
	require.Empty(t, bank2.sends, "nothing is redirected when there is no backing to protect")

	bank3 := newRecordingBank()
	guard3 := newGovBurnGuard(bank3, fixedVaults{total: math.NewInt(1)})
	require.NoError(t, guard3.BurnCoins(ctx, cointypes.ModuleName, burn))
	require.Equal(t, math.NewInt(1_000), bank3.burned[cointypes.ModuleName])
	require.Empty(t, bank3.sends)
}

func (govBurnGuard) burnedTotal(b *recordingBank) math.Int {
	if v, ok := b.burned[govtypes.ModuleName]; ok {
		return v
	}
	return math.ZeroInt()
}
