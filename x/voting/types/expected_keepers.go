// SPDX-License-Identifier: Apache-2.0

package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	credentialstypes "github.com/Port-PHI/phi-chain/x/credentials/types"
)

// CredentialsKeeper reads a credential template for its issuer BBS+ key; never a specific anchor (anonymous ballot).
type CredentialsKeeper interface {
	GetTemplate(ctx sdk.Context, id string) (credentialstypes.CredentialTemplate, bool)
}
