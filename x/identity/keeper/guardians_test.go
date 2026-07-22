// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"fmt"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/identity/keeper"
	"github.com/Port-PHI/phi-chain/x/identity/types"
)

func saltFor(label string) []byte {
	s := make([]byte, types.GuardianSaltLen)
	copy(s, "salt-"+label)
	return s
}

func commitFor(guardianDID, label string) []byte {
	return types.GuardianCommitment(guardianDID, saltFor(label))
}

func guardianPool(t *testing.T, ctx sdk.Context, msg types.MsgServer, n int) (dids []string, commitments [][]byte) {
	t.Helper()
	for i := 0; i < n; i++ {
		label := fmt.Sprintf("guardian-%d", i)
		ctrl := someAddr(fmt.Sprintf("guardian-ctrl-%-6d", i))
		did := registerActive(t, ctx, msg, ctrl, label, []byte("bio-"+label))
		dids = append(dids, did)
		commitments = append(commitments, commitFor(did, label))
	}
	return dids, commitments
}

func guardianCtrl(i int) string { return someAddr(fmt.Sprintf("guardian-ctrl-%-6d", i)) }

func guardianPoolLabelled(t *testing.T, ctx sdk.Context, msg types.MsgServer, n int, pool string) (dids []string, commitments [][]byte) {
	t.Helper()
	for i := 0; i < n; i++ {
		label := fmt.Sprintf("%s-%d", pool, i)
		did := registerActive(t, ctx, msg, guardianCtrlLabelled(pool, i), label, []byte("bio-"+label))
		dids = append(dids, did)
		commitments = append(commitments, commitFor(did, label))
	}
	return dids, commitments
}

func guardianCtrlLabelled(pool string, i int) string {
	if pool == "guardian" {
		return guardianCtrl(i)
	}
	return someAddr(fmt.Sprintf("%s-ctrl-%-6d", pool, i))
}

func setupGuardians(t *testing.T, poolSize int) (sdk.Context, keeper.Keeper, types.MsgServer, string, string, []string, [][]byte) {
	t.Helper()
	ctx, k, msg := setupIdentity(t)
	ctx = ctx.WithBlockTime(time.Unix(1_000_000, 0))
	ctrl := someAddr("owner_______________")
	did := registerActive(t, ctx, msg, ctrl, "owner", []byte("bio-owner"))
	dids, commitments := guardianPool(t, ctx, msg, poolSize)
	return ctx, k, msg, ctrl, did, dids, commitments
}

func requireGuardiansSet(t *testing.T, ctx sdk.Context, did string, count, threshold int) {
	t.Helper()
	for _, e := range ctx.EventManager().Events() {
		if e.Type != types.EventTypeGuardiansSet {
			continue
		}
		got := map[string]string{}
		for _, a := range e.Attributes {
			got[a.Key] = a.Value
		}
		require.Equal(t, did, got[types.AttributeKeyDID])
		require.Equal(t, fmt.Sprintf("%d", count), got[types.AttributeKeyGuardianCount])
		require.Equal(t, fmt.Sprintf("%d", threshold), got[types.AttributeKeyThreshold])
		return
	}
	t.Fatalf("GuardiansSet event was not emitted")
}

// The controller sets a 3-of-5 commitment set: it is stored, GuardiansSet is emitted, and the query exposes only the SHAPE (count + threshold) — never the commitments or any guardian identity.
func TestSetGuardians_ControllerSets3of5(t *testing.T) {
	ctx, k, msg, ctrl, did, _, commitments := setupGuardians(t, 5)

	ctx = ctx.WithEventManager(sdk.NewEventManager())
	_, err := msg.SetGuardians(ctx, &types.MsgSetGuardians{
		Controller: ctrl, Did: did, Commitments: commitments, Threshold: 3,
	})
	require.NoError(t, err)

	gs, found := k.GetGuardians(ctx, did)
	require.True(t, found)
	require.Equal(t, commitments, gs.Commitments)
	require.Equal(t, uint32(3), gs.Threshold)
	requireGuardiansSet(t, ctx, did, 5, 3)

	res, err := k.Guardians(ctx, &types.QueryGuardiansRequest{Did: did})
	require.NoError(t, err)
	require.Equal(t, uint32(5), res.CommitmentCount)
	require.Equal(t, uint32(3), res.Threshold)
}

// Only the DID's current controller may set its guardians.
func TestSetGuardians_NonControllerRejected(t *testing.T) {
	ctx, _, msg, _, did, _, commitments := setupGuardians(t, 3)

	_, err := msg.SetGuardians(ctx, &types.MsgSetGuardians{
		Controller: someAddr("attacker____________"), Did: did, Commitments: commitments, Threshold: 2,
	})
	require.ErrorIs(t, err, types.ErrUnauthorized)
}

// Threshold bounds: 0 and > len are rejected; N-of-N is allowed.
func TestSetGuardians_ThresholdBounds(t *testing.T) {
	ctx, k, msg, ctrl, did, _, commitments := setupGuardians(t, 3)
	set := func(threshold uint32) error {
		_, err := msg.SetGuardians(ctx, &types.MsgSetGuardians{
			Controller: ctrl, Did: did, Commitments: commitments, Threshold: threshold,
		})
		return err
	}

	require.ErrorIs(t, set(0), types.ErrInvalidGuardians, "threshold 0 is rejected")
	require.ErrorIs(t, set(4), types.ErrInvalidGuardians, "threshold > guardian count is rejected")

	require.NoError(t, set(3), "N-of-N is a legitimate configuration")
	gs, _ := k.GetGuardians(ctx, did)
	require.Equal(t, uint32(3), gs.Threshold)
}

// A set larger than the governed max_guardians cap is rejected; exactly at the cap is accepted.
func TestSetGuardians_ExceedsMaxGuardians(t *testing.T) {
	ctx, k, msg, ctrl, did, _, commitments := setupGuardians(t, int(types.DefaultMaxGuardians)+1)
	require.Equal(t, types.DefaultMaxGuardians, k.GetParams(ctx).MaxGuardians)

	_, err := msg.SetGuardians(ctx, &types.MsgSetGuardians{
		Controller: ctrl, Did: did, Commitments: commitments, Threshold: 2,
	})
	require.ErrorIs(t, err, types.ErrInvalidGuardians)

	_, err = msg.SetGuardians(ctx, &types.MsgSetGuardians{
		Controller: ctrl, Did: did, Commitments: commitments[:types.DefaultMaxGuardians], Threshold: 2,
	})
	require.NoError(t, err)
}

// Malformed commitments are rejected: wrong length, and duplicates within the set.
func TestSetGuardians_CommitmentShape(t *testing.T) {
	ctx, _, msg, ctrl, did, _, commitments := setupGuardians(t, 3)

	_, err := msg.SetGuardians(ctx, &types.MsgSetGuardians{
		Controller: ctrl, Did: did, Commitments: [][]byte{{0x01, 0x02}}, Threshold: 1,
	})
	require.ErrorIs(t, err, types.ErrInvalidGuardians, "a commitment must be exactly 32 bytes")

	dup := [][]byte{commitments[0], commitments[1], commitments[0]}
	_, err = msg.SetGuardians(ctx, &types.MsgSetGuardians{
		Controller: ctrl, Did: did, Commitments: dup, Threshold: 2,
	})
	require.ErrorIs(t, err, types.ErrInvalidGuardians, "duplicate commitment is rejected")
}

// Under the commitment scheme the chain CANNOT judge guardian eligibility at set time: it does not know which DIDs the commitments commit to.
func TestSetGuardians_EligibilityNotCheckedAtSetTime(t *testing.T) {
	ctx, k, msg, ctrl, did, _, _ := setupGuardians(t, 1)

	opaque := [][]byte{
		commitFor(didFor("ghost"), "ghost"),
		commitFor(did, "self"),
	}
	_, err := msg.SetGuardians(ctx, &types.MsgSetGuardians{
		Controller: ctrl, Did: did, Commitments: opaque, Threshold: 1,
	})
	require.NoError(t, err, "the chain cannot see the committed DIDs, so it cannot reject them here")

	gs, found := k.GetGuardians(ctx, did)
	require.True(t, found)
	require.Len(t, gs.Commitments, 2)
}

// SetGuardians is a full replace, never a merge.
func TestSetGuardians_FullReplace(t *testing.T) {
	ctx, k, msg, ctrl, did, _, commitments := setupGuardians(t, 5)

	_, err := msg.SetGuardians(ctx, &types.MsgSetGuardians{
		Controller: ctrl, Did: did, Commitments: commitments, Threshold: 3,
	})
	require.NoError(t, err)

	replacement := commitments[3:5]
	_, err = msg.SetGuardians(ctx, &types.MsgSetGuardians{
		Controller: ctrl, Did: did, Commitments: replacement, Threshold: 2,
	})
	require.NoError(t, err)

	gs, _ := k.GetGuardians(ctx, did)
	require.Equal(t, replacement, gs.Commitments, "the whole set is replaced, not merged")
	require.Len(t, gs.Commitments, 2)
	require.Equal(t, uint32(2), gs.Threshold)
}

// A suspended DID's controller cannot set guardians: the keeper refuses (and the Slice-1 ante guard already blocks that controller from transacting at all).
func TestSetGuardians_SuspendedControllerRejected(t *testing.T) {
	ctx, k, msg, ctrl, did, _, commitments := setupGuardians(t, 3)
	auth := k.GetAuthority()

	_, err := msg.UpdateStatus(ctx, &types.MsgUpdateStatus{
		Authority: auth, Did: did, NewStatus: types.DID_STATUS_SUSPENDED,
	})
	require.NoError(t, err)
	require.True(t, k.HasNonActiveDID(ctx, ctrl), "ante status guard blocks a suspended DID's controller")

	_, err = msg.SetGuardians(ctx, &types.MsgSetGuardians{
		Controller: ctrl, Did: did, Commitments: commitments, Threshold: 2,
	})
	require.ErrorIs(t, err, types.ErrInvalidGuardians)
}

// Guardian commitment sets round-trip through ExportGenesis/InitGenesis unchanged.
func TestGenesis_GuardianSetRoundTrip(t *testing.T) {
	ctx, k, msg, ctrl, did, _, commitments := setupGuardians(t, 5)
	ctx = ctx.WithBlockHeight(1)

	_, err := msg.SetGuardians(ctx, &types.MsgSetGuardians{
		Controller: ctrl, Did: did, Commitments: commitments, Threshold: 3,
	})
	require.NoError(t, err)

	gs := k.ExportGenesis(ctx)
	require.Len(t, gs.GuardianSets, 1)
	require.NoError(t, gs.Validate())

	ctx2, k2, _ := setupIdentity(t)
	k2.InitGenesis(ctx2, *gs)
	got, found := k2.GetGuardians(ctx2, did)
	require.True(t, found)
	require.Equal(t, commitments, got.Commitments)
	require.Equal(t, uint32(3), got.Threshold)
}
