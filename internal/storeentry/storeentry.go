// SPDX-License-Identifier: Apache-2.0

// Package storeentry validates the raw (key, value) records a module genesis carries verbatim: keys must be confined to exported store prefixes, and values must decode correctly (a wrong-width counter reads fail-soft as zero and would silently change a rule).
package storeentry

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
)

// MaxValueLen caps any single raw genesis value, refusing a payload sized to bloat the store under an unbounded marker prefix.
const MaxValueLen = 4096

// KV is one raw genesis record.
type KV struct {
	Key   []byte
	Value []byte
}

// Rule confines one store prefix and states how values under it must decode; a nil Value means an opaque payload this validator cannot judge.
type Rule struct {
	Name   string
	Prefix []byte
	Value  func([]byte) error
}

// Validate checks every entry against the rule set: a key must lie STRICTLY under one prefix, the most specific (longest) match decides (order-independent), and an unmatched key is an error.
func Validate(field string, entries []KV, rules ...Rule) error {
	// Longest-prefix-first on a copy so `match` takes the first hit as the most specific rule.
	ordered := append([]Rule(nil), rules...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return len(ordered[i].Prefix) > len(ordered[j].Prefix)
	})

	seen := make(map[string]int, len(entries))
	for i, e := range entries {
		// Duplicate key: a second record overwrites the first on import, order-dependently.
		if prev, dup := seen[string(e.Key)]; dup {
			return fmt.Errorf("%s[%d]: key %X duplicates %s[%d]", field, i, e.Key, field, prev)
		}
		seen[string(e.Key)] = i

		// Ceiling on every value, whatever its rule.
		if len(e.Value) > MaxValueLen {
			return fmt.Errorf("%s[%d]: value is %d bytes, exceeds the %d-byte ceiling", field, i, len(e.Value), MaxValueLen)
		}

		rule, ok := match(e.Key, ordered)
		if !ok {
			return fmt.Errorf("%s[%d]: key is not under an exported store prefix", field, i)
		}
		if rule.Value == nil {
			continue
		}
		if err := rule.Value(e.Value); err != nil {
			return fmt.Errorf("%s[%d]: %s: %w", field, i, rule.Name, err)
		}
	}
	return nil
}

func match(key []byte, rules []Rule) (Rule, bool) {
	for _, r := range rules {
		if len(key) > len(r.Prefix) && string(key[:len(r.Prefix)]) == string(r.Prefix) {
			return r, true
		}
	}
	return Rule{}, false
}

// FixedLen requires an exact width (a short counter decodes to zero instead of failing).
func FixedLen(n int) func([]byte) error {
	return func(v []byte) error {
		if len(v) != n {
			return fmt.Errorf("value is %d bytes, want %d", len(v), n)
		}
		return nil
	}
}

// NonEmpty requires a value to carry something (a marker key with an empty value imports indistinguishably from a deletion).
func NonEmpty() func([]byte) error {
	return func(v []byte) error {
		if len(v) == 0 {
			return fmt.Errorf("value is empty")
		}
		return nil
	}
}

// BoundedUint64 requires an eight-byte value decoding to a big-endian uint64 strictly below ceiling (a saturated MaxUint64 epoch would wrap to zero and un-retire tallies).
func BoundedUint64(ceiling uint64) func([]byte) error {
	return func(v []byte) error {
		if len(v) != 8 {
			return fmt.Errorf("value is %d bytes, want 8", len(v))
		}
		n := binary.BigEndian.Uint64(v)
		if n >= ceiling {
			return fmt.Errorf("value %d is at or above the ceiling %d", n, ceiling)
		}
		return nil
	}
}

// Uint64NoOverflow requires an eight-byte counter strictly below math.MaxUint64 so one increment cannot wrap it to zero.
func Uint64NoOverflow() func([]byte) error { return BoundedUint64(math.MaxUint64) }

// OneOfLen requires one of several exact widths, for a record whose encoding has grown and whose reader still accepts the older shorter form.
func OneOfLen(ns ...int) func([]byte) error {
	return func(v []byte) error {
		for _, n := range ns {
			if len(v) == n {
				return nil
			}
		}
		return fmt.Errorf("value is %d bytes, want one of %v", len(v), ns)
	}
}
