// SPDX-License-Identifier: Apache-2.0

package keeper_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

// The register handler asserts the institution-id bound itself — not only ValidateBasic — so a caller that reaches the keeper by another path cannot slip an empty or over-long id into SetInstitution.
func TestRegisterInstitution_IdBoundEnforcedInHandler(t *testing.T) {
	f := setup(t)

	_, err := f.msg.RegisterInstitution(f.ctx, &types.MsgRegisterInstitution{
		Operator: f.oper.String(), Id: "", License: "LIC", Admin: f.admin.String(),
		InstitutionType: types.INSTITUTION_TYPE_FINANCIAL,
	})
	require.ErrorIs(t, err, types.ErrInstitutionNotFound)

	long := strings.Repeat("x", types.MaxInstitutionIDLen+1)
	_, err = f.msg.RegisterInstitution(f.ctx, &types.MsgRegisterInstitution{
		Operator: f.oper.String(), Id: long, License: "LIC", Admin: f.admin.String(),
		InstitutionType: types.INSTITUTION_TYPE_FINANCIAL,
	})
	require.ErrorIs(t, err, types.ErrIDTooLong)
	require.False(t, f.k.HasInstitution(f.ctx, long), "no institution must be written on a rejected id")

	ok := strings.Repeat("y", types.MaxInstitutionIDLen)
	_, err = f.msg.RegisterInstitution(f.ctx, &types.MsgRegisterInstitution{
		Operator: f.oper.String(), Id: ok, License: "LIC", Admin: f.admin.String(),
		InstitutionType: types.INSTITUTION_TYPE_FINANCIAL,
	})
	require.NoError(t, err, "a 255-byte id is within bound and accepted")
}
