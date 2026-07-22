// SPDX-License-Identifier: Apache-2.0

// Package keeper implements the x/voting state machine: credential-gated, anonymous, nullifier-deduplicated polls; eligibility proofs verified via the phicrypto.Verifier port.
package keeper

import (
	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Port-PHI/phi-chain/phicrypto"
	"github.com/Port-PHI/phi-chain/x/voting/types"
)

// Keeper manages the x/voting state.
type Keeper struct {
	cdc               codec.BinaryCodec
	storeKey          storetypes.StoreKey
	authority         string
	credentialsKeeper types.CredentialsKeeper
	verifier          phicrypto.Verifier
	// soundnessEnforced gates real tallying (from build-tag const VotingSoundnessEnforced; tests inject).
	soundnessEnforced bool
}

// NewKeeper builds a new keeper.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeKey storetypes.StoreKey,
	authority string,
	credentials types.CredentialsKeeper,
	verifier phicrypto.Verifier,
	soundnessEnforced bool,
) Keeper {
	return Keeper{
		cdc:               cdc,
		storeKey:          storeKey,
		authority:         authority,
		credentialsKeeper: credentials,
		verifier:          verifier,
		soundnessEnforced: soundnessEnforced,
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

func (k Keeper) SetElection(ctx sdk.Context, e types.Election) {
	ctx.KVStore(k.storeKey).Set(types.ElectionKey(e.Id), k.cdc.MustMarshal(&e))
}

func (k Keeper) GetElection(ctx sdk.Context, id string) (types.Election, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.ElectionKey(id))
	if bz == nil {
		return types.Election{}, false
	}
	var e types.Election
	k.cdc.MustUnmarshal(bz, &e)
	return e, true
}

func (k Keeper) HasElection(ctx sdk.Context, id string) bool {
	return ctx.KVStore(k.storeKey).Has(types.ElectionKey(id))
}

// IterateElections iterates all elections; returning true stops.
func (k Keeper) IterateElections(ctx sdk.Context, cb func(types.Election) bool) {
	it := storetypes.KVStorePrefixIterator(ctx.KVStore(k.storeKey), types.ElectionPrefix)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var e types.Election
		k.cdc.MustUnmarshal(it.Value(), &e)
		if cb(e) {
			break
		}
	}
}

// SetBallot stores a ballot; the ballot key doubles as the nullifier-used marker.
func (k Keeper) SetBallot(ctx sdk.Context, b types.Ballot) {
	ctx.KVStore(k.storeKey).Set(types.BallotKey(b.ElectionId, b.Nullifier), k.cdc.MustMarshal(&b))
}

// HasBallot reports whether a nullifier has already voted in an election.
func (k Keeper) HasBallot(ctx sdk.Context, electionID string, nullifier []byte) bool {
	return ctx.KVStore(k.storeKey).Has(types.BallotKey(electionID, nullifier))
}

// IterateBallots iterates all ballots; returning true stops.
func (k Keeper) IterateBallots(ctx sdk.Context, cb func(types.Ballot) bool) {
	it := storetypes.KVStorePrefixIterator(ctx.KVStore(k.storeKey), types.BallotPrefix)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var b types.Ballot
		k.cdc.MustUnmarshal(it.Value(), &b)
		if cb(b) {
			break
		}
	}
}
