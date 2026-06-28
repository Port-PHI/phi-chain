// SPDX-License-Identifier: Apache-2.0

package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	credentialstypes "github.com/Port-PHI/phi-chain/x/credentials/types"
)

// CredentialsKeeper is the x/credentials dependency. x/disclosure verifies a
// selective-disclosure proof against an anchored credential: it reads the anchor
// (to check it exists and is ACTIVE) and the credential template (to recover the
// issuer's BBS+ public key the proof must verify against).
type CredentialsKeeper interface {
	GetAnchor(ctx sdk.Context, hash []byte) (credentialstypes.CredentialAnchor, bool)
	GetTemplate(ctx sdk.Context, id string) (credentialstypes.CredentialTemplate, bool)
}
