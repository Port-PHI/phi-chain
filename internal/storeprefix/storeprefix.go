// SPDX-License-Identifier: Apache-2.0

// Package storeprefix is how a module declares the complete set of KVStore prefixes it owns and what genesis does with each; a genesis export drops a keyspace silently, so the set is declared and tested rather than remembered.
package storeprefix

// Carry is what genesis does with a keyspace, stated per prefix so "not exported" and "forgotten" no longer look identical.
type Carry int

const (
	// CarryExact: exported and imported byte for byte (the default).
	CarryExact Carry = iota
	// CarryDerived: not exported but REBUILT on import from exported state; must come back identical record-for-record to a from-scratch recompute (deep-compared, since a partial loss is non-empty).
	CarryDerived
	// CarryDropped: deliberately not carried; must come back EMPTY (asserted).
	CarryDropped
)

// Prefix is one KVStore prefix a module owns.
type Prefix struct {
	Name string
	// Bytes is the prefix exactly as the module's key constructors emit it.
	Bytes []byte
	Carry Carry
	// Reason justifies anything other than CarryExact and is required for those.
	Reason string
}

// Names returns the declared prefix names.
func Names(ps []Prefix) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name
	}
	return out
}

// Under narrows a store dump to records lying under one prefix.
func Under(dump map[string]string, prefix []byte) map[string]string {
	out := map[string]string{}
	for k, v := range dump {
		if len(k) >= len(prefix) && k[:len(prefix)] == string(prefix) {
			out[k] = v
		}
	}
	return out
}
