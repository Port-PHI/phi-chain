// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/institutions/keeper"
	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

// The pin: an attacker splitting a balance across many fresh addresses is bound by a single cap.
func TestRedeemCap_FreshAddressesShareOneCap(t *testing.T) {
	f := setupDIDCap(t, capUphiForTest, map[string]string{})
	f.registerAndAttest(t, "bank-a", 1_000_000)

	const attackers = 8
	addrs := make([]sdk.AccAddress, 0, attackers)
	for i := 0; i < attackers; i++ {
		a := sdk.AccAddress([]byte(fmt.Sprintf("fresh-address-%06d", i)))
		addrs = append(addrs, a)
		f.mintTo(t, "bank-a", a, "5000", fmt.Sprintf("dep-%d", i))
	}

	require.NoError(t, f.redeem("bank-a", addrs[0], "1000", "red-0"),
		"the first unidentified holder may spend the full daily allowance")

	for i := 1; i < attackers; i++ {
		err := f.redeem("bank-a", addrs[i], "1000", fmt.Sprintf("red-%d", i))
		require.ErrorIs(t, err, types.ErrCapExceeded,
			"address %d must not receive a fresh daily cap merely by being a new address", i)
	}
}

// A legitimate holder WITH an identity is unaffected: they have their own bucket, and an attacker exhausting the unidentified bucket cannot touch it.
func TestRedeemCap_IdentifiedHolderIsUnaffectedByUnidentifiedTraffic(t *testing.T) {
	alice := sdk.AccAddress([]byte("alice_______________"))
	f := setupDIDCap(t, capUphiForTest, map[string]string{alice.String(): "did:phi:alice"})
	f.registerAndAttest(t, "bank-a", 1_000_000)

	attacker := sdk.AccAddress([]byte("attacker____________"))
	f.mintTo(t, "bank-a", attacker, "5000", "dep-attacker")
	require.NoError(t, f.redeem("bank-a", attacker, "1000", "red-attacker"))
	require.ErrorIs(t, f.redeem("bank-a", attacker, "1", "red-attacker-2"), types.ErrCapExceeded)

	f.mintTo(t, "bank-a", alice, "5000", "dep-alice")
	require.NoError(t, f.redeem("bank-a", alice, "1000", "red-alice"),
		"a holder with an identity must not be crowded out of their own cap")
	require.ErrorIs(t, f.redeem("bank-a", alice, "1", "red-alice-2"), types.ErrCapExceeded,
		"and is still bound by it")
}

// Two identified humans are independent of each other — the cap is per human, not global.
func TestRedeemCap_IdentifiedHoldersAreIndependent(t *testing.T) {
	alice := sdk.AccAddress([]byte("alice_______________"))
	bob := sdk.AccAddress([]byte("bob_________________"))
	f := setupDIDCap(t, capUphiForTest, map[string]string{
		alice.String(): "did:phi:alice",
		bob.String():   "did:phi:bob",
	})
	f.registerAndAttest(t, "bank-a", 1_000_000)
	f.mintTo(t, "bank-a", alice, "5000", "dep-alice")
	f.mintTo(t, "bank-a", bob, "5000", "dep-bob")

	require.NoError(t, f.redeem("bank-a", alice, "1000", "red-alice"))
	require.NoError(t, f.redeem("bank-a", bob, "1000", "red-bob"),
		"one human exhausting their cap must not consume another's")
}

// One human cannot escape their own cap by moving to a second address, whether or not that second address resolves to the same identity.
func TestRedeemCap_OneHumanCannotEscapeViaASecondAddress(t *testing.T) {
	primary := sdk.AccAddress([]byte("alice-primary_______"))
	secondary := sdk.AccAddress([]byte("alice-secondary_____"))

	f := setupDIDCap(t, capUphiForTest, map[string]string{
		primary.String():   "did:phi:alice",
		secondary.String(): "did:phi:alice",
	})
	f.registerAndAttest(t, "bank-a", 1_000_000)
	f.mintTo(t, "bank-a", primary, "5000", "dep-1")
	f.mintTo(t, "bank-a", secondary, "5000", "dep-2")

	require.NoError(t, f.redeem("bank-a", primary, "1000", "red-1"))
	require.ErrorIs(t, f.redeem("bank-a", secondary, "1000", "red-2"), types.ErrCapExceeded,
		"a second address of the same human shares that human's cap")
}

// A holder who registers an identity moves OUT of the shared bucket, which is the incentive the shared bucket creates.
func TestRedeemCap_RegisteringAnIdentityMovesTheHolderToTheirOwnBucket(t *testing.T) {
	holder := sdk.AccAddress([]byte("late-registrant_____"))
	dids := map[string]string{}
	f := setupDIDCap(t, capUphiForTest, dids)
	f.registerAndAttest(t, "bank-a", 1_000_000)
	f.mintTo(t, "bank-a", holder, "10000", "dep-1")

	require.NoError(t, f.redeem("bank-a", holder, "1000", "red-1"))
	require.ErrorIs(t, f.redeem("bank-a", holder, "1", "red-2"), types.ErrCapExceeded)

	dids[holder.String()] = "did:phi:late"

	require.NoError(t, f.redeem("bank-a", holder, "1000", "red-3"),
		"an identified holder is counted under their own subject")
}

// Redeeming moves coin between accounts and burns against a vault; it never mints.
func TestRedeemCap_SharedBucketPreservesSolvency(t *testing.T) {
	f := setupDIDCap(t, capUphiForTest, map[string]string{})
	f.registerAndAttest(t, "bank-a", 1_000_000)

	for i := 0; i < 4; i++ {
		a := sdk.AccAddress([]byte(fmt.Sprintf("solvency-addr-%05d", i)))
		f.mintTo(t, "bank-a", a, "5000", fmt.Sprintf("dep-%d", i))
		_ = f.redeem("bank-a", a, "1000", fmt.Sprintf("red-%d", i))
		msg, broken := keeper.SolvencyInvariant(f.k)(f.ctx)
		require.False(t, broken, "solvency broken after attempt %d: %s", i, msg)
	}
}
