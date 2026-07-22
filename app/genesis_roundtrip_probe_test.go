// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"testing"

	"cosmossdk.io/log"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/internal/storeprefix"
	"github.com/Port-PHI/phi-chain/internal/storeprefix/prefixtest"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	credentialstypes "github.com/Port-PHI/phi-chain/x/credentials/types"
	disclosuretypes "github.com/Port-PHI/phi-chain/x/disclosure/types"
	governancetypes "github.com/Port-PHI/phi-chain/x/governance/types"
	identitytypes "github.com/Port-PHI/phi-chain/x/identity/types"
	insttypes "github.com/Port-PHI/phi-chain/x/institutions/types"
	votingtypes "github.com/Port-PHI/phi-chain/x/voting/types"
)

type phiModule struct {
	name     string
	storeKey string
	declared []storeprefix.Prefix
}

func phiModules() []phiModule {
	return []phiModule{
		{"coin", cointypes.StoreKey, cointypes.AllStorePrefixes()},
		{"credentials", credentialstypes.StoreKey, credentialstypes.AllStorePrefixes()},
		{"disclosure", disclosuretypes.StoreKey, disclosuretypes.AllStorePrefixes()},
		{"governance", governancetypes.StoreKey, governancetypes.AllStorePrefixes()},
		{"identity", identitytypes.StoreKey, identitytypes.AllStorePrefixes()},
		{"institutions", insttypes.StoreKey, insttypes.AllStorePrefixes()},
		{"voting", votingtypes.StoreKey, votingtypes.AllStorePrefixes()},
	}
}

func (c *roundTripChain) writeCtx() sdk.Context {
	return sdk.NewContext(c.app.CommitMultiStore(), cmtproto.Header{
		Height: c.app.LastBlockHeight(), Time: genesisChainTime,
	}, false, log.NewNopLogger())
}

func (c *roundTripChain) commit() { c.app.CommitMultiStore().Commit() }

func (c *roundTripChain) dumpAll() map[string]map[string]string {
	ctx := c.ctx()
	out := map[string]map[string]string{}
	for _, m := range phiModules() {
		out[m.name] = prefixtest.Dump(ctx, c.app.GetKey(m.storeKey))
	}
	return out
}

var drainedWithinABlock = map[string]map[string]string{
	"governance": {
		"prune_queue": "drained by the module's EndBlock within the block after it is written",
	},
	"institutions": {
		"counter_prune_cursor": "consumed by the cap-counter sweep it paces",
		"removal_queue":        "has no genesis representation (a mid-drain removal exports as already-completed) and is drained by the module's BeginBlock removal sweep, so a whole-app round-trip that runs a block cannot observe it",
	},
}

func appSkipReason(module, prefix string) (string, bool) {
	reason, ok := drainedWithinABlock[module][prefix]
	return reason, ok
}

func keepObservable(module string, declared []storeprefix.Prefix) []storeprefix.Prefix {
	out := make([]storeprefix.Prefix, 0, len(declared))
	for _, p := range declared {
		if _, skipped := appSkipReason(module, p.Name); !skipped {
			out = append(out, p)
		}
	}
	return out
}

// A keyspace the whole-app net cannot observe must still be one its module carries.
func TestNet_AppSkipsAreCarriedByTheirModule(t *testing.T) {
	for module, prefixes := range drainedWithinABlock {
		declared := map[string]storeprefix.Prefix{}
		for _, m := range phiModules() {
			if m.name != module {
				continue
			}
			for _, p := range m.declared {
				declared[p.Name] = p
			}
		}
		require.NotEmpty(t, declared, "unknown module %q in the app skip list", module)

		for name, reason := range prefixes {
			p, ok := declared[name]
			require.True(t, ok, "%s: skipped prefix %q is not declared by the module", module, name)
			require.NotEmpty(t, reason, "%s/%s: a skip must say why", module, name)

			require.NotEqual(t, storeprefix.CarryDerived, p.Carry,
				"%s/%s is rebuilt on import, so it must stay observable to the whole-app net — a derived "+
					"keyspace that comes back empty has no other witness", module, name)
			if p.Carry == storeprefix.CarryDropped {
				require.NotEmpty(t, p.Reason, "%s/%s: a dropped keyspace must say why", module, name)
			}
		}
	}
}

func requireEveryKeyspaceSeeded(t *testing.T, before map[string]map[string]string) {
	t.Helper()
	for _, m := range phiModules() {
		t.Run("seeded/"+m.name, func(t *testing.T) {
			prefixtest.RequireSeeded(t, before[m.name], keepObservable(m.name, m.declared))
		})
	}
}

func requireEveryKeyspaceRoundTripped(t *testing.T, before, after map[string]map[string]string) {
	t.Helper()
	for _, m := range phiModules() {
		t.Run("roundtrip/"+m.name, func(t *testing.T) {
			require.NotNil(t, after[m.name], "module %q has no store on the restarted chain", m.name)
			observable := keepObservable(m.name, m.declared)
			prefixtest.RequireRoundTrip(t, observable,
				withoutSkipped(m.name, m.declared, before[m.name]),
				withoutSkipped(m.name, m.declared, after[m.name]))
		})
	}
}

func withoutSkipped(module string, declared []storeprefix.Prefix, dump map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range dump {
		skipped := false
		for _, p := range declared {
			if _, ok := appSkipReason(module, p.Name); !ok {
				continue
			}
			if len(k) >= len(p.Bytes) && k[:len(p.Bytes)] == string(p.Bytes) {
				skipped = true
				break
			}
		}
		if !skipped {
			out[k] = v
		}
	}
	return out
}
