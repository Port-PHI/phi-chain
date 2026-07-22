// SPDX-License-Identifier: Apache-2.0

// Package keeper is the verify-only x/disclosure state machine: verifies BBS+ selective-disclosure proofs against anchored, non-revoked credentials, stores nothing — "verify and forget".
package keeper

import (
	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/phicrypto"
	"github.com/Port-PHI/phi-chain/x/disclosure/types"
)

type Keeper struct {
	cdc               codec.BinaryCodec
	storeKey          storetypes.StoreKey
	authority         string
	credentialsKeeper types.CredentialsKeeper
	verifier          phicrypto.Verifier
}

// NewKeeper builds a keeper; verifier is the phi-crypto port.
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

func (k Keeper) GetAuthority() string { return k.authority }

func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}

func (k Keeper) GetParams(ctx sdk.Context) (p types.Params) {
	bz := ctx.KVStore(k.storeKey).Get(types.ParamsKey)
	if bz == nil {
		return types.DefaultParams()
	}
	k.cdc.MustUnmarshal(bz, &p)
	return p
}

// SetParams validates then stores the parameters.
func (k Keeper) SetParams(ctx sdk.Context, p types.Params) error {
	if err := p.Validate(); err != nil {
		return err
	}
	ctx.KVStore(k.storeKey).Set(types.ParamsKey, k.cdc.MustMarshal(&p))
	return nil
}
