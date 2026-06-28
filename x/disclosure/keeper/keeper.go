// SPDX-License-Identifier: Apache-2.0

// Package keeper implements the x/disclosure verify-only state machine. It holds
// no per-disclosure records: it authoritatively verifies BBS+ selective-disclosure
// proofs (via the phicrypto.Verifier port) against anchored, non-revoked
// credentials read from x/credentials. Proofs are exchanged off-chain and never
// stored — "verify and forget".
package keeper

import (
	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/phicrypto"
	"github.com/Port-PHI/phi-chain/x/disclosure/types"
)

// Keeper manages the x/disclosure parameters and verification logic.
type Keeper struct {
	cdc               codec.BinaryCodec
	storeKey          storetypes.StoreKey
	authority         string
	credentialsKeeper types.CredentialsKeeper
	verifier          phicrypto.Verifier
}

// NewKeeper builds a new keeper. verifier is the phi-crypto port; in production
// app wiring it is phicrypto.Default() (Disabled unless built with the cgo tag),
// and tests inject phicrypto.Fake.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeKey storetypes.StoreKey,
	authority string,
	credentials types.CredentialsKeeper,
	verifier phicrypto.Verifier,
) Keeper {
	return Keeper{
		cdc:               cdc,
		storeKey:          storeKey,
		authority:         authority,
		credentialsKeeper: credentials,
		verifier:          verifier,
	}
}

// GetAuthority returns the governance authority address.
func (k Keeper) GetAuthority() string { return k.authority }

// Logger returns the module logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}

// GetParams returns the current parameters.
func (k Keeper) GetParams(ctx sdk.Context) (p types.Params) {
	bz := ctx.KVStore(k.storeKey).Get(types.ParamsKey)
	if bz == nil {
		return types.DefaultParams()
	}
	k.cdc.MustUnmarshal(bz, &p)
	return p
}

// SetParams stores the parameters after validation.
func (k Keeper) SetParams(ctx sdk.Context, p types.Params) error {
	if err := p.Validate(); err != nil {
		return err
	}
	ctx.KVStore(k.storeKey).Set(types.ParamsKey, k.cdc.MustMarshal(&p))
	return nil
}
