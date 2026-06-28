// SPDX-License-Identifier: Apache-2.0

package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	identitytypes "github.com/Port-PHI/phi-chain/x/identity/types"
)

// IdentityKeeper is the x/identity dependency. The credentials module resolves a
// DID to its document to check that it is active and that the message signer
// controls it, and to recover its public key for signature verification.
type IdentityKeeper interface {
	GetIdentity(ctx sdk.Context, did string) (identitytypes.DIDDocument, bool)
}
