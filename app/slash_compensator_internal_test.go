// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
)

type fakeSupplyBank struct{ supply math.Int }

func (b *fakeSupplyBank) GetSupply(_ context.Context, denom string) sdk.Coin {
	return sdk.NewCoin(denom, b.supply)
}
func (b *fakeSupplyBank) mint(a math.Int) { b.supply = b.supply.Add(a) }
func (b *fakeSupplyBank) burn(a math.Int) { b.supply = b.supply.Sub(a) }

type fakePenaltyEscrow struct {
	bank *fakeSupplyBank
	got  math.Int
}

func (p *fakePenaltyEscrow) RedirectSlashedToPenalty(_ sdk.Context, slashed math.Int) error {
	p.got = slashed
	p.bank.mint(slashed)
	return nil
}

type fakeStakingSlasher struct {
	slashingtypes.StakingKeeper
	bank          *fakeSupplyBank
	burn          math.Int // total uphi the SDK burns across the whole slash
	validatorOnly math.Int // the validator-direct amount Slash returns
}

func (f fakeStakingSlasher) SlashWithInfractionReason(_ context.Context, _ sdk.ConsAddress, _, _ int64, _ math.LegacyDec, _ stakingtypes.Infraction) (math.Int, error) {
	f.bank.burn(f.burn)
	return f.validatorOnly, nil
}

func (f fakeStakingSlasher) Slash(_ context.Context, _ sdk.ConsAddress, _, _ int64, _ math.LegacyDec) (math.Int, error) {
	f.bank.burn(f.burn)
	return f.validatorOnly, nil
}

func unitCtx(t *testing.T) sdk.Context {
	t.Helper()
	key := storetypes.NewKVStoreKey("slashcomp_test")
	return testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_slashcomp")).Ctx
}

// The compensator must re-mint the whole-slash supply delta (not just the validator-direct burn) to keep supply unchanged.
func TestSlashCompensator_RemintsWholeSlashBurnNotJustValidatorDirect(t *testing.T) {
	ctx := unitCtx(t)
	const initial = 10_000_000

	bank := &fakeSupplyBank{supply: math.NewInt(initial)}
	pen := &fakePenaltyEscrow{bank: bank, got: math.ZeroInt()}
	sk := fakeStakingSlasher{bank: bank, burn: math.NewInt(73_421), validatorOnly: math.NewInt(40_000)}
	comp := newSlashCompensator(sk, bank, pen)

	burned, err := comp.SlashWithInfractionReason(ctx, sdk.ConsAddress("cons"), 5, 100,
		math.LegacyNewDecWithPrec(5, 2), stakingtypes.Infraction_INFRACTION_DOUBLE_SIGN)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(40_000), burned, "returns the SDK's validator-direct burn unchanged")
	require.Equal(t, math.NewInt(73_421), pen.got, "penalty must receive the FULL measured burn, not 40,000")
	require.Equal(t, math.NewInt(initial), bank.supply, "total uphi supply must be unchanged across the slash")
}

// The no-infraction-reason Slash entry is compensated identically.
func TestSlashCompensator_SlashEntryAlsoCompensated(t *testing.T) {
	ctx := unitCtx(t)
	bank := &fakeSupplyBank{supply: math.NewInt(5_000_000)}
	pen := &fakePenaltyEscrow{bank: bank, got: math.ZeroInt()}
	sk := fakeStakingSlasher{bank: bank, burn: math.NewInt(12_345), validatorOnly: math.NewInt(12_345)}
	comp := newSlashCompensator(sk, bank, pen)

	_, err := comp.Slash(ctx, sdk.ConsAddress("cons"), 5, 100, math.LegacyNewDecWithPrec(5, 2))
	require.NoError(t, err)
	require.Equal(t, math.NewInt(12_345), pen.got)
	require.Equal(t, math.NewInt(5_000_000), bank.supply)
}

// A zero-burn slash must not call the penalty escrow.
func TestSlashCompensator_NoBurnNoRemint(t *testing.T) {
	ctx := unitCtx(t)
	bank := &fakeSupplyBank{supply: math.NewInt(1_000_000)}
	pen := &fakePenaltyEscrow{bank: bank, got: math.ZeroInt()}
	sk := fakeStakingSlasher{bank: bank, burn: math.ZeroInt(), validatorOnly: math.ZeroInt()}
	comp := newSlashCompensator(sk, bank, pen)

	_, err := comp.SlashWithInfractionReason(ctx, sdk.ConsAddress("cons"), 5, 100,
		math.LegacyZeroDec(), stakingtypes.Infraction_INFRACTION_DOWNTIME)
	require.NoError(t, err)
	require.True(t, pen.got.IsZero(), "no burn → no penalty mint")
	require.Equal(t, math.NewInt(1_000_000), bank.supply)
}

// Guard: the compensator denom must equal the staking bond denom (uphi).
func TestSlashCompensator_MeasuresBondDenom(t *testing.T) {
	require.Equal(t, "uphi", cointypes.Denom)
}
