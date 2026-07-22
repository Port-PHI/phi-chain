// SPDX-License-Identifier: Apache-2.0

package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	credentialstypes "github.com/Port-PHI/phi-chain/x/credentials/types"
)

// CredentialsKeeper is the x/credentials dependency.
type CredentialsKeeper interface {
	GetAnchor(ctx sdk.Context, hash []byte) (credentialstypes.CredentialAnchor, bool)
	GetTemplate(ctx sdk.Context, id string) (credentialstypes.CredentialTemplate, bool)
}
