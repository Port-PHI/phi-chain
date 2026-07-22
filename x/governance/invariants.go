// SPDX-License-Identifier: Apache-2.0

package governance

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/governance/keeper"
	"github.com/Port-PHI/phi-chain/x/governance/types"
)

func RegisterInvariants(ir sdk.InvariantRegistry, k keeper.Keeper) {
	ir.RegisterRoute(types.ModuleName, "turnout-within-frozen-basis", TurnoutWithinFrozenBasisInvariant(k))
}

// TurnoutWithinFrozenBasisInvariant is the one-human-one-vote guarantee: turnout numerator stays a subset of the once-frozen denominator.
func TurnoutWithinFrozenBasisInvariant(k keeper.Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		for _, proposalID := range k.TalliedProposalIDs(ctx) {
			running := k.GetRunningTally(ctx, proposalID)

			counted := uint64(0)
			k.IterateCountedVotes(ctx, proposalID, func(_ []byte, marker byte) bool {
				if marker != types.IneligibleVoteMarker {
					counted++
				}
				return false
			})

			basis, hasBasis := k.GetProposalEligibility(ctx, proposalID)
			if !hasBasis {
				if counted == 0 && running.Turnout == 0 && sumCounts(running) == 0 {
					continue // no basis and nothing counted against one: consistent
				}
				return sdk.FormatInvariant(types.ModuleName, "turnout-within-frozen-basis",
					fmt.Sprintf("proposal %d holds %d counted ballot(s) and turnout %d with NO frozen eligibility basis",
						proposalID, counted, running.Turnout)), true
			}

			if running.Turnout > basis.Denominator {
				return sdk.FormatInvariant(types.ModuleName, "turnout-within-frozen-basis",
					fmt.Sprintf("proposal %d turnout %d exceeds its frozen denominator %d",
						proposalID, running.Turnout, basis.Denominator)), true
			}

			if total := sumCounts(running); total != running.Turnout {
				return sdk.FormatInvariant(types.ModuleName, "turnout-within-frozen-basis",
					fmt.Sprintf("proposal %d per-option counts sum to %d but turnout is %d",
						proposalID, total, running.Turnout)), true
			}

			if counted != running.Turnout {
				return sdk.FormatInvariant(types.ModuleName, "turnout-within-frozen-basis",
					fmt.Sprintf("proposal %d records %d counted ballot(s) but turnout is %d",
						proposalID, counted, running.Turnout)), true
			}
		}
		return "", false
	}
}

func sumCounts(t keeper.RunningTally) uint64 {
	var total uint64
	for _, n := range t.Counts {
		total += n
	}
	return total
}
