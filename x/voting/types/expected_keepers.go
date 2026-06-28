// SPDX-License-Identifier: Apache-2.0

package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	credentialstypes "github.com/Port-PHI/phi-chain/x/credentials/types"
)

// CredentialsKeeper is the x/credentials dependency. x/voting reads a credential
// template to recover the issuer BBS+ public key that eligibility proofs verify
// against. It deliberately does NOT read a specific credential anchor: an
// anonymous ballot proves possession of *some* credential of the template without
// identifying which one.
type CredentialsKeeper interface {
	GetTemplate(ctx sdk.Context, id string) (credentialstypes.CredentialTemplate, bool)
}
