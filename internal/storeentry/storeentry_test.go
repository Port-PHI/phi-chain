// SPDX-License-Identifier: Apache-2.0

package storeentry_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/internal/storeentry"
)

var probePrefix = []byte{0x42}

func key(suffix string) []byte { return append(append([]byte{}, probePrefix...), suffix...) }

func u64(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

func epochRule() storeentry.Rule {
	return storeentry.Rule{Name: "epoch", Prefix: probePrefix, Value: storeentry.Uint64NoOverflow()}
}

func TestStoreEntry_DuplicateKeyRejected(t *testing.T) {
	entries := []storeentry.KV{
		{Key: key("a"), Value: u64(1)},
		{Key: key("a"), Value: u64(2)},
	}
	err := storeentry.Validate("f", entries, epochRule())
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicat")

	require.NoError(t, storeentry.Validate("f", []storeentry.KV{
		{Key: key("a"), Value: u64(1)}, {Key: key("b"), Value: u64(2)},
	}, epochRule()))
}

// An oversized value is refused even under a marker rule that imposes no width of its own.
func TestStoreEntry_OversizedValueRejected(t *testing.T) {
	marker := storeentry.Rule{Name: "marker", Prefix: probePrefix, Value: storeentry.NonEmpty()}
	big := bytes.Repeat([]byte{1}, storeentry.MaxValueLen+1)
	err := storeentry.Validate("f", []storeentry.KV{{Key: key("x"), Value: big}}, marker)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ceiling")

	ok := bytes.Repeat([]byte{1}, storeentry.MaxValueLen)
	require.NoError(t, storeentry.Validate("f", []storeentry.KV{{Key: key("y"), Value: ok}}, marker))
}

// A saturated uint64 is refused, though it is exactly eight bytes and passes every width check.
func TestStoreEntry_SaturatedUint64Rejected(t *testing.T) {
	err := storeentry.Validate("f", []storeentry.KV{{Key: key("e"), Value: u64(math.MaxUint64)}}, epochRule())
	require.Error(t, err)
	require.Contains(t, err.Error(), "ceiling")

	require.NoError(t, storeentry.Validate("f", []storeentry.KV{{Key: key("e"), Value: u64(math.MaxUint64 - 1)}}, epochRule()))
	require.NoError(t, storeentry.Validate("f", []storeentry.KV{{Key: key("e"), Value: u64(7)}}, epochRule()))

	require.Error(t, storeentry.Validate("f", []storeentry.KV{{Key: key("e"), Value: []byte{1, 2, 3}}}, epochRule()))
}

// BoundedUint64 honours a tighter ceiling than the saturated value.
func TestStoreEntry_BoundedUint64TighterCeiling(t *testing.T) {
	rule := storeentry.Rule{Name: "bounded", Prefix: probePrefix, Value: storeentry.BoundedUint64(100)}
	require.NoError(t, storeentry.Validate("f", []storeentry.KV{{Key: key("a"), Value: u64(99)}}, rule))
	require.Error(t, storeentry.Validate("f", []storeentry.KV{{Key: key("a"), Value: u64(100)}}, rule))
}

// When one rule's prefix is a prefix of another's, the LONGEST match must decide in either caller order.
func TestStoreEntry_MatchIsIndependentOfRuleOrder(t *testing.T) {
	short := storeentry.Rule{Name: "epoch", Prefix: []byte{0x42}, Value: storeentry.Uint64NoOverflow()}
	long := storeentry.Rule{Name: "marker", Prefix: []byte{0x42, 0x99}, Value: storeentry.NonEmpty()}

	markerKey := []byte{0x42, 0x99, 0x01}
	oneByte := []byte{0x07}
	require.NoError(t, storeentry.Validate("f", []storeentry.KV{{Key: markerKey, Value: oneByte}}, long, short),
		"marker-first order accepts the specific match")
	require.NoError(t, storeentry.Validate("f", []storeentry.KV{{Key: markerKey, Value: oneByte}}, short, long),
		"short-prefix-first must reach the SAME decision — matching no longer depends on rule order")

	epochKey := []byte{0x42, 0x01}
	require.Error(t, storeentry.Validate("f", []storeentry.KV{{Key: epochKey, Value: oneByte}}, short, long),
		"a key outside the longer prefix is width-checked by the epoch rule")
	require.NoError(t, storeentry.Validate("f", []storeentry.KV{{Key: epochKey, Value: u64(3)}}, long, short),
		"an 8-byte counter under the short prefix is accepted in either order")
}
