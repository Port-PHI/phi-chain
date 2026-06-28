// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	instkeeper "github.com/Port-PHI/phi-chain/x/institutions/keeper"
	insttypes "github.com/Port-PHI/phi-chain/x/institutions/types"
)

// Regression: a validator slash at a PAST infraction height burns the validator's bonded tokens
// AND the slashed portion of every active unbonding-delegation and redelegation. uphi is the bond
// denom and is fully backed by institution vaults, so all of those burns must be compensated or the
// global solvency invariant breaks (which would then freeze the institution mint/redeem rail and arm
// a permissionless chain halt). This test drives a real slash through x/slashing → the staking keeper
// the slash-compensation wrapper guards, with both an unbonding delegation and a redelegation active,
// and asserts: (a) total uphi supply is unchanged, (b) the solvency invariant holds, and (c) a
// subsequent InstitutionMint and InstitutionRedeem still succeed.
func TestSlashCompensation_PreservesSupplyAndSolvencyWithUnbondingAndRedelegation(t *testing.T) {
	a := newTestApp(t)
	blockTime := time.Unix(1_700_000_000, 0).UTC()
	const infractionHeight = 5
	// Operate directly on the mounted stores (no InitChain, which would require a genesis validator set).
	ctx := a.NewUncachedContext(false, cmtproto.Header{Height: 100, Time: blockTime})

	sk := a.StakingKeeper
	bondDenom := cointypes.Denom

	// Staking params: bond denom = uphi, 14-day unbonding (so the entries created below stay active).
	sp := stakingtypes.DefaultParams()
	sp.BondDenom = bondDenom
	sp.UnbondingTime = 14 * 24 * time.Hour
	require.NoError(t, sk.SetParams(ctx, sp))
	// Distribution bookkeeping the delegation hooks touch (normally set by distr InitGenesis).
	require.NoError(t, a.DistrKeeper.FeePool.Set(ctx, distrtypes.InitialFeePool()))

	// Accounts. The penalty destination is a dedicated account so the re-mint is observable.
	f1 := sdk.AccAddress([]byte("val1-self-bond-acct_"))  // val1 operator + self-delegator
	f2 := sdk.AccAddress([]byte("val2-self-bond-acct_"))  // val2 operator + self-delegator
	del := sdk.AccAddress([]byte("delegator-account___")) // undelegates + redelegates
	holder := sdk.AccAddress([]byte("redeem-holder-acct__"))
	operator := sdk.AccAddress([]byte("inst-operator-acct__"))
	penalty := sdk.AccAddress([]byte("penalty-destination_"))
	val1 := sdk.ValAddress(f1)
	val2 := sdk.ValAddress(f2)

	// Institution params: operator + penalty destination. penalty_destination receives the re-minted
	// slashed stake; it is gov-updatable via MsgUpdateInstitutionParams.
	require.NoError(t, a.InstitutionsKeeper.SetParams(ctx, insttypes.Params{
		Operator:           operator.String(),
		PenaltyDestination: penalty.String(),
		PhiToToman:         insttypes.DefaultPhiToToman,
	}))

	// A bootstrap institution backs all the uphi we are about to create for staking, so the solvency
	// invariant holds from the start (this mirrors `phid genesis add-institution`).
	imsg := instkeeper.NewMsgServerImpl(a.InstitutionsKeeper)
	_, err := imsg.RegisterInstitution(ctx, &insttypes.MsgRegisterInstitution{
		Operator: operator.String(), Id: "bank-a", License: "LIC-1", Admin: operator.String(),
		VaultAccount: "v", VaultApi: "x", Bond: "0", InstitutionType: insttypes.INSTITUTION_TYPE_FINANCIAL,
	})
	require.NoError(t, err)
	_, err = imsg.PublishInstitutionAttestation(ctx, &insttypes.MsgPublishInstitutionAttestation{
		Admin: operator.String(), Institution: "bank-a", AttestedReserve: "1000000000000", // ample headroom
	})
	require.NoError(t, err)

	// Fund the staking participants by minting backed uphi (1 toman = 10 uphi). Every uphi minted here
	// is matched by vault toman, so solvency holds.
	mint := func(to sdk.AccAddress, toman, ref string) {
		_, err := imsg.InstitutionMint(ctx, &insttypes.MsgInstitutionMint{
			Admin: operator.String(), Institution: "bank-a", Recipient: to.String(),
			AmountToman: toman, DepositRef: ref,
		})
		require.NoError(t, err)
	}
	mint(f1, "10000000", "fund-f1")  // 100 PHI = 100,000,000 uphi
	mint(f2, "10000000", "fund-f2")  // 100 PHI
	mint(del, "6000000", "fund-del") // 60 PHI

	// Create and bond two validators (keeper-level, so the DID hook — which only fires via the staking
	// msg server — does not apply). Self-delegate, then run the staking EndBlocker to bond them.
	bootstrapValidator := func(oper sdk.AccAddress, valAddr sdk.ValAddress, secret string, selfBondUphi math.Int) {
		consPub := ed25519.GenPrivKeyFromSecret([]byte(secret)).PubKey()
		val, err := stakingtypes.NewValidator(valAddr.String(), consPub, stakingtypes.Description{Moniker: secret})
		require.NoError(t, err)
		val.Status = stakingtypes.Unbonded
		require.NoError(t, sk.SetValidator(ctx, val))
		require.NoError(t, sk.SetValidatorByConsAddr(ctx, val))
		require.NoError(t, sk.SetNewValidatorByPowerIndex(ctx, val))
		// Initialize the validator's distribution info (the msg-server path's AfterValidatorCreated hook,
		// which we bypass to avoid the DID gate); the delegation hooks below read it.
		require.NoError(t, a.DistrKeeper.Hooks().AfterValidatorCreated(ctx, valAddr))
		_, err = sk.Delegate(ctx, oper, selfBondUphi, stakingtypes.Unbonded, val, true)
		require.NoError(t, err)
	}
	phi := math.NewIntFromUint64(cointypes.UphiPerPhi) // 1 PHI in uphi
	bootstrapValidator(f1, val1, "val1-cons-secret", phi.MulRaw(100))
	bootstrapValidator(f2, val2, "val2-cons-secret", phi.MulRaw(100))

	// The delegator bonds 60 PHI to val1, then we bond the set.
	val1Obj, err := sk.GetValidator(ctx, val1)
	require.NoError(t, err)
	_, err = sk.Delegate(ctx, del, phi.MulRaw(60), stakingtypes.Unbonded, val1Obj, true)
	require.NoError(t, err)
	_, err = sk.EndBlocker(ctx)
	require.NoError(t, err)

	// Active unbonding delegation (20 PHI) and redelegation (20 PHI, val1→val2) from the delegator,
	// both created at the current height (>= infractionHeight) so the past-height slash hits them.
	_, _, err = sk.Undelegate(ctx, del, val1, math.LegacyNewDecFromInt(phi.MulRaw(20)))
	require.NoError(t, err)
	_, err = sk.BeginRedelegation(ctx, del, val1, val2, math.LegacyNewDecFromInt(phi.MulRaw(20)))
	require.NoError(t, err)

	// Confirm the entries exist (the test is only meaningful if both burn sources are present).
	ubd, err := sk.GetUnbondingDelegation(ctx, del, val1)
	require.NoError(t, err)
	require.NotEmpty(t, ubd.Entries, "an unbonding delegation must be active")
	reds, err := sk.GetRedelegationsFromSrcValidator(ctx, val1)
	require.NoError(t, err)
	require.NotEmpty(t, reds, "a redelegation must be active")

	supplyBefore := a.BankKeeper.GetSupply(ctx, bondDenom).Amount
	penaltyBefore := a.BankKeeper.GetBalance(ctx, penalty, bondDenom).Amount
	_, broken := instkeeper.SolvencyInvariant(a.InstitutionsKeeper)(ctx)
	require.False(t, broken, "solvency must hold before the slash")

	// Slash val1 5% for a double-sign at a PAST infraction height. This goes through the slashing
	// keeper → the wrapped staking keeper, burning the bonded remainder + the slashed unbonding +
	// the slashed redelegation, all of which the wrapper measures and re-mints to penalty_destination.
	consAddr1 := sdk.ConsAddress(ed25519.GenPrivKeyFromSecret([]byte("val1-cons-secret")).PubKey().Address())
	val1Power := sk.PowerReduction(ctx) // 1 power == PowerReduction uphi
	power := phi.MulRaw(160).Quo(val1Power).Int64()
	require.NoError(t, a.SlashingKeeper.SlashWithInfractionReason(
		ctx, consAddr1, math.LegacyNewDecWithPrec(5, 2), power, infractionHeight,
		stakingtypes.Infraction_INFRACTION_DOUBLE_SIGN))

	// (a) Total uphi supply is unchanged across the whole slash.
	supplyAfter := a.BankKeeper.GetSupply(ctx, bondDenom).Amount
	require.Equal(t, supplyBefore.String(), supplyAfter.String(),
		"slash must conserve total uphi supply (bonded + unbonding + redelegation burns all compensated)")

	// The compensation actually happened (a real burn occurred and was re-minted to penalty_destination).
	penaltyAfter := a.BankKeeper.GetBalance(ctx, penalty, bondDenom).Amount
	require.True(t, penaltyAfter.GT(penaltyBefore),
		"penalty destination must receive the re-minted slashed stake (burn was non-zero)")

	// (b) The solvency invariant still holds.
	_, broken = instkeeper.SolvencyInvariant(a.InstitutionsKeeper)(ctx)
	require.False(t, broken, "solvency must hold after the slash")

	// (c) The institution mint/redeem rail is NOT frozen: both still pass their write-path assertSolvency.
	_, err = imsg.InstitutionMint(ctx, &insttypes.MsgInstitutionMint{
		Admin: operator.String(), Institution: "bank-a", Recipient: holder.String(),
		AmountToman: "1000", DepositRef: "post-slash-mint",
	})
	require.NoError(t, err, "InstitutionMint must still succeed after a slash (rail not frozen)")
	_, err = imsg.InstitutionRedeem(ctx, &insttypes.MsgInstitutionRedeem{
		Admin: holder.String(), Institution: "bank-a", Holder: holder.String(),
		AmountToman: "500", RedeemRef: "post-slash-redeem",
	})
	require.NoError(t, err, "InstitutionRedeem must still succeed after a slash (rail not frozen)")

	_, broken = instkeeper.SolvencyInvariant(a.InstitutionsKeeper)(ctx)
	require.False(t, broken, "solvency must hold after the post-slash mint/redeem")
}
