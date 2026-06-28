// SPDX-License-Identifier: Apache-2.0

package types

import (
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// TestMsgRequestFxEntry_ValidateBasic_License covers the case where an fx onboarding request must carry a
// non-empty, bounded license (mirroring MsgRegisterInstitution).
func TestMsgRequestFxEntry_ValidateBasic_License(t *testing.T) {
	applicant := sdk.AccAddress([]byte("applicant___________")).String()
	base := func() *MsgRequestFxEntry {
		return &MsgRequestFxEntry{Applicant: applicant, FxId: "ex-1", GuarantorId: "bank-a", License: "LIC-1"}
	}
	require.NoError(t, base().ValidateBasic())

	noLic := base()
	noLic.License = ""
	require.Error(t, noLic.ValidateBasic(), "an empty license must be rejected")

	bigLic := base()
	bigLic.License = strings.Repeat("x", MaxLicenseLen+1)
	require.Error(t, bigLic.ValidateBasic(), "an over-length license must be rejected")
}
