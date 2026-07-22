// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"bytes"
	"testing"
)

func oldSeparatorJoin(parts ...[]byte) []byte { return bytes.Join(parts, []byte{0x00}) }

// TestDepositMessage_LengthPrefixBindsFields pins the provenance-binding property of the deposit message.
func TestDepositMessage_LengthPrefixBindsFields(t *testing.T) {
	const inst, recipient = "bank-a", "phi1recipient"

	amountA, refA := "100", "200\x00abc"
	amountB, refB := "100\x00200", "abc"

	oldA := oldSeparatorJoin([]byte(depositMessageDomain), []byte(inst), []byte(recipient), []byte(amountA), []byte(refA))
	oldB := oldSeparatorJoin([]byte(depositMessageDomain), []byte(inst), []byte(recipient), []byte(amountB), []byte(refB))
	if !bytes.Equal(oldA, oldB) {
		t.Fatal("precondition: the two tuples must collide under the old 0x00-join")
	}

	newA := buildDepositMessage(inst, recipient, amountA, refA, false, "", "", "")
	newB := buildDepositMessage(inst, recipient, amountB, refB, false, "", "", "")
	if bytes.Equal(newA, newB) {
		t.Fatal("length-prefixed framing must give the two colliding tuples distinct bytes")
	}

	signedOverA := func(msg []byte) bool { return bytes.Equal(msg, newA) }
	if !signedOverA(newA) {
		t.Fatal("a signature over A must still validate A")
	}
	if signedOverA(newB) {
		t.Fatal("a signature over A must not validate B once the fields are length-prefixed")
	}

	fx := buildDepositMessage(inst, recipient, amountA, refA, true, "BTC", "3", "0xabc")
	if bytes.Equal(fx, newA) {
		t.Fatal("the fx provenance tail must change the signed bytes")
	}
}
