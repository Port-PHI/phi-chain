// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"bytes"
	"fmt"
	"sort"

	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/x/identity/types"
)

// RegisterInvariants registers the identity module's consensus-halting invariants with x/crisis.
func RegisterInvariants(ir sdk.InvariantRegistry, k Keeper) {
	ir.RegisterRoute(types.ModuleName, "uniqueness-bijection", UniquenessBijectionInvariant(k))
	ir.RegisterRoute(types.ModuleName, "controller-index", ControllerIndexInvariant(k))
	ir.RegisterRoute(types.ModuleName, "status-validity", StatusValidityInvariant(k))
	ir.RegisterRoute(types.ModuleName, "eligibility-index", EligibilityIndexInvariant(k))
}

// AllInvariants checks every registered identity invariant in a single pass.
func AllInvariants(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		for _, inv := range []sdk.Invariant{
			UniquenessBijectionInvariant(k),
			ControllerIndexInvariant(k),
			StatusValidityInvariant(k),
			EligibilityIndexInvariant(k),
		} {
			if msg, broken := inv(ctx); broken {
				return msg, broken
			}
		}
		return "", false
	}
}

// UniquenessBijectionInvariant asserts a one-to-one correspondence between identities and uniqueness markers (one-human-one-DID), forward and reverse.
func UniquenessBijectionInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		store := ctx.KVStore(k.storeKey)

		var badMsg string
		var broken bool
		k.IterateIdentities(ctx, func(d types.DIDDocument) bool {
			val := store.Get(types.UniquenessKey(d.UniquenessHash))
			switch {
			case val == nil:
				broken = true
				badMsg = fmt.Sprintf("identity %q has no uniqueness marker for hash %x", d.Did, d.UniquenessHash)
			case string(val) != d.Did:
				broken = true
				badMsg = fmt.Sprintf("identity %q uniqueness marker points to %q", d.Did, string(val))
			}
			return broken
		})
		if broken {
			return sdk.FormatInvariant(types.ModuleName, "uniqueness-bijection", badMsg), true
		}

		it := storetypes.KVStorePrefixIterator(store, types.UniquenessPrefix)
		defer it.Close()
		for ; it.Valid(); it.Next() {
			hash := it.Key()[len(types.UniquenessPrefix):]
			did := string(it.Value())
			d, found := k.GetIdentity(ctx, did)
			switch {
			case !found:
				return sdk.FormatInvariant(types.ModuleName, "uniqueness-bijection",
					fmt.Sprintf("uniqueness marker %x points to missing DID %q", hash, did)), true
			case !bytes.Equal(d.UniquenessHash, hash):
				return sdk.FormatInvariant(types.ModuleName, "uniqueness-bijection",
					fmt.Sprintf("uniqueness marker %x resolves to DID %q whose hash is %x", hash, did, d.UniquenessHash)), true
			}
		}
		return sdk.FormatInvariant(types.ModuleName, "uniqueness-bijection", "ok"), false
	}
}

// ControllerIndexInvariant asserts the controller→DID secondary index agrees with the identity records, forward and reverse.
func ControllerIndexInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		store := ctx.KVStore(k.storeKey)

		var badMsg string
		var broken bool
		k.IterateIdentities(ctx, func(d types.DIDDocument) bool {
			if !store.Has(types.ControllerIndexKey(d.Controller, d.Did)) {
				broken = true
				badMsg = fmt.Sprintf("identity %q (controller %q) has no controller-index entry", d.Did, d.Controller)
			}
			return broken
		})
		if broken {
			return sdk.FormatInvariant(types.ModuleName, "controller-index", badMsg), true
		}

		it := storetypes.KVStorePrefixIterator(store, types.ControllerIndexPrefix)
		defer it.Close()
		for ; it.Valid(); it.Next() {
			controller, did, ok := parseControllerIndexKey(it.Key())
			if !ok {
				return sdk.FormatInvariant(types.ModuleName, "controller-index",
					fmt.Sprintf("malformed controller-index key %x", it.Key())), true
			}
			d, found := k.GetIdentity(ctx, did)
			switch {
			case !found:
				return sdk.FormatInvariant(types.ModuleName, "controller-index",
					fmt.Sprintf("controller-index entry (controller %q) points to missing DID %q", controller, did)), true
			case d.Controller != controller:
				return sdk.FormatInvariant(types.ModuleName, "controller-index",
					fmt.Sprintf("stale controller-index entry (controller %q → DID %q); DID's controller is %q", controller, did, d.Controller)), true
			}
		}
		return sdk.FormatInvariant(types.ModuleName, "controller-index", "ok"), false
	}
}

// StatusValidityInvariant asserts every identity's status is ACTIVE, SUSPENDED or REVOKED (UNSPECIFIED is not a legal state).
func StatusValidityInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var badMsg string
		var broken bool
		k.IterateIdentities(ctx, func(d types.DIDDocument) bool {
			switch d.Status {
			case types.DID_STATUS_ACTIVE, types.DID_STATUS_SUSPENDED, types.DID_STATUS_REVOKED:
			default:
				broken = true
				badMsg = fmt.Sprintf("identity %q has invalid status %d", d.Did, d.Status)
			}
			return broken
		})
		return sdk.FormatInvariant(types.ModuleName, "status-validity", badMsg), broken
	}
}

// EligibilityIndexInvariant asserts the one-human-one-vote eligibility structures (record, age-ordered mirror, and stored total — the quorum denominator) agree with the identity records.
func EligibilityIndexInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		store := ctx.KVStore(k.storeKey)
		fail := func(format string, args ...any) (string, bool) {
			return sdk.FormatInvariant(types.ModuleName, "eligibility-index", fmt.Sprintf(format, args...)), true
		}

		want := map[string]int64{}
		k.IterateIdentities(ctx, func(d types.DIDDocument) bool {
			if d.Status != types.DID_STATUS_REVOKED {
				if cur, ok := want[d.Controller]; !ok || d.CreatedAt < cur {
					want[d.Controller] = d.CreatedAt
				}
			}
			return false
		})

		// Walked in SORTED order (consensus-critical): metered reads exit on first mismatch, so map-order randomization would diverge gas_used across validators and fork the chain on identical state.
		controllers := make([]string, 0, len(want))
		for controller := range want {
			controllers = append(controllers, controller)
		}
		sort.Strings(controllers)

		for _, controller := range controllers {
			oldest := want[controller]
			got, _, ok := types.DecodeControllerEligibility(store.Get(types.ControllerEligibilityKey(controller)))
			if !ok {
				return fail("controller %q has a non-revoked DID but no readable eligibility record", controller)
			}
			if got != oldest {
				return fail("controller %q eligibility record is %d, oldest non-revoked DID is %d", controller, got, oldest)
			}
			if !store.Has(types.EligibilityByAgeKey(oldest, controller)) {
				return fail("controller %q eligibility record is not mirrored in the age index", controller)
			}
		}

		var records uint64
		it := storetypes.KVStorePrefixIterator(store, types.ControllerEligibilityPrefix)
		defer it.Close()
		for ; it.Valid(); it.Next() {
			controller := string(it.Key()[len(types.ControllerEligibilityPrefix):])
			if _, ok := want[controller]; !ok {
				return fail("controller %q has an eligibility record but no non-revoked DID", controller)
			}
			records++
		}
		if total := k.EligibleControllerTotal(ctx); total != records {
			return fail("eligible-controller total is %d, but %d records exist", total, records)
		}

		var mirrored uint64
		mit := storetypes.KVStoreReversePrefixIterator(store, types.EligibilityByAgePrefix)
		defer mit.Close()
		for ; mit.Valid(); mit.Next() {
			if _, ok := types.CreatedAtFromEligibilityByAgeKey(mit.Key()); !ok {
				return fail("malformed age-index key %x", mit.Key())
			}
			mirrored++
		}
		if mirrored != records {
			return fail("age index holds %d entries, but %d eligibility records exist", mirrored, records)
		}

		return sdk.FormatInvariant(types.ModuleName, "eligibility-index", "ok"), false
	}
}

func parseControllerIndexKey(key []byte) (controller, did string, ok bool) {
	if len(key) < len(types.ControllerIndexPrefix) {
		return "", "", false
	}
	body := key[len(types.ControllerIndexPrefix):]
	i := bytes.IndexByte(body, 0x00)
	if i < 0 {
		return "", "", false
	}
	return string(body[:i]), string(body[i+1:]), true
}
