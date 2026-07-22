// SPDX-License-Identifier: Apache-2.0

package types

import "encoding/binary"

// CanonicalMessage builds an INJECTIVE signing string: u32be(len(domain))‖domain‖u32be(len(f))‖f‖… The length prefix on every field (domain included) is what makes two different field tuples unable to produce the same bytes.
func CanonicalMessage(domain string, fields ...[]byte) []byte {
	size := 4 + len(domain)
	for _, f := range fields {
		size += 4 + len(f)
	}
	out := make([]byte, 0, size)
	out = appendLengthPrefixed(out, []byte(domain))
	for _, f := range fields {
		out = appendLengthPrefixed(out, f)
	}
	return out
}

func appendLengthPrefixed(dst, field []byte) []byte {
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(field)))
	dst = append(dst, lenBuf[:]...)
	return append(dst, field...)
}
