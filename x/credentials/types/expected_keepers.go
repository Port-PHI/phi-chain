// SPDX-License-Identifier: Apache-2.0

package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	identitytypes "github.com/Port-PHI/phi-chain/x/identity/types"
)

// IdentityKeeper is the x/identity dependency: resolves a DID to its document.
type IdentityKeeper interface {
	GetIdentity(ctx sdk.Context, did string) (identitytypes.DIDDocument, bool)
}
