// SPDX-License-Identifier: Apache-2.0

// Package keeper implements the x/voting state machine: credential-gated,
// anonymous, nullifier-deduplicated polls. Eligibility proofs are verified via
// the phicrypto.Verifier port (never hand-rolled crypto).
//
// SECURITY NOTE: the per-election nullifier is bound to the eligibility proof by
// phi-crypto's Semaphore binding layer (semaphore::bind_nonce, reached via
// VerifySemaphoreVote): a proof is accepted only for the exact nullifier it was
// bound to, so a single proof yields at most one accepted nullifier per election
// (no third-party replay, no two-nullifiers-from-one-proof) and the chain
// deduplicates reused nullifiers. The remaining one-human-one-vote gap is the
// zero-knowledge derivation proof that nullifier = H(secret, election) for a
// *signed-claim* secret — a holder able to generate multiple fresh proofs from one
// credential can still mint distinct nullifiers until that vetted SNARK circuit
// lands (tracked in phi-crypto semaphore.rs). Ballot secrecy (encrypted ballots +
// threshold decryption) is likewise deferred; tallies are public and live.
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
	// soundnessEnforced gates real vote tallying. Production wiring sets it from the
	// build-tag const VotingSoundnessEnforced (false until the derivation-proof SNARK ships); tests
	// inject it directly.
	soundnessEnforced bool
}

// NewKeeper builds a new keeper. verifier is the phi-crypto port; in production
// app wiring it is phicrypto.Default() (Disabled unless built with the cgo tag),
// and tests inject phicrypto.Fake. soundnessEnforced gates real tallying: production
// passes the build-tag const VotingSoundnessEnforced (false until the derivation-proof SNARK ships).
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

// GetAuthority returns the governance authority address.
func (k Keeper) GetAuthority() string { return k.authority }

// Logger returns the module logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}

// --- params ---

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

// --- elections ---

// SetElection stores an election.
func (k Keeper) SetElection(ctx sdk.Context, e types.Election) {
	ctx.KVStore(k.storeKey).Set(types.ElectionKey(e.Id), k.cdc.MustMarshal(&e))
}

// GetElection reads an election by id.
func (k Keeper) GetElection(ctx sdk.Context, id string) (types.Election, bool) {
	bz := ctx.KVStore(k.storeKey).Get(types.ElectionKey(id))
	if bz == nil {
		return types.Election{}, false
	}
	var e types.Election
	k.cdc.MustUnmarshal(bz, &e)
	return e, true
}

// HasElection reports whether an election id exists.
func (k Keeper) HasElection(ctx sdk.Context, id string) bool {
	return ctx.KVStore(k.storeKey).Has(types.ElectionKey(id))
}

// IterateElections iterates all elections; returning true stops the loop.
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

// --- ballots / nullifiers ---

// SetBallot stores a ballot (also the nullifier-used marker).
func (k Keeper) SetBallot(ctx sdk.Context, b types.Ballot) {
	ctx.KVStore(k.storeKey).Set(types.BallotKey(b.ElectionId, b.Nullifier), k.cdc.MustMarshal(&b))
}

// HasBallot reports whether a nullifier has already voted in an election.
func (k Keeper) HasBallot(ctx sdk.Context, electionID string, nullifier []byte) bool {
	return ctx.KVStore(k.storeKey).Has(types.BallotKey(electionID, nullifier))
}

// IterateBallots iterates all ballots; returning true stops the loop.
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
