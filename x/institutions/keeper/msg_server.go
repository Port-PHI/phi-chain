// SPDX-License-Identifier: Apache-2.0

package keeper

import (
	"bytes"
	"context"

	"cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/Port-PHI/phi-chain/phicrypto"
	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	"github.com/Port-PHI/phi-chain/x/institutions/types"
)

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns a MsgServer implementation.
func NewMsgServerImpl(k Keeper) types.MsgServer {
	return &msgServer{Keeper: k}
}

var _ types.MsgServer = msgServer{}

// canManageRegistry reports whether the signer may add/remove/freeze an institution.
// During the bootstrap phase: the operator set in Params; always: governance (x/gov).
func (k Keeper) canManageRegistry(ctx sdk.Context, signer string) bool {
	if signer == k.authority {
		return true
	}
	params := k.GetParams(ctx)
	if k.identityKeeper.BootstrapPhase(ctx) && params.Operator != "" && signer == params.Operator {
		return true
	}
	return false
}

// RegisterInstitution registers a new financial institution.
func (k msgServer) RegisterInstitution(goCtx context.Context, msg *types.MsgRegisterInstitution) (*types.MsgRegisterInstitutionResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if !k.canManageRegistry(ctx, msg.Operator) {
		return nil, errors.Wrap(types.ErrUnauthorized, "only operator (bootstrap) or governance may register institutions")
	}
	if k.HasInstitution(ctx, msg.Id) {
		return nil, errors.Wrapf(types.ErrInstitutionExists, "id %s", msg.Id)
	}
	// Reject UNSPECIFIED (0) or out-of-range types: an unspecified type would silently mint as
	// a financial institution (see validateFxMetadata) and bypass the fx provenance rules.
	if msg.InstitutionType != types.INSTITUTION_TYPE_FINANCIAL && msg.InstitutionType != types.INSTITUTION_TYPE_FX {
		return nil, errors.Wrapf(types.ErrInvalidInstitutionType, "institution_type=%d", msg.InstitutionType)
	}

	bond := "0"
	if msg.Bond != "" {
		bond = msg.Bond
	}
	inst := types.Institution{
		Id:              msg.Id,
		License:         msg.License,
		Admin:           msg.Admin,
		VaultAccount:    msg.VaultAccount,
		VaultApi:        msg.VaultApi,
		Bond:            bond,
		Status:          types.INSTITUTION_STATUS_HEALTHY,
		VaultBalance:    "0",
		AttestedReserve: "0",
		PausedMint:      false,
		InstitutionType: msg.InstitutionType,
	}
	k.SetInstitution(ctx, inst)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeInstitutionRegistered,
		sdk.NewAttribute(types.AttributeKeyInstitution, msg.Id),
		sdk.NewAttribute(types.AttributeKeyAdmin, msg.Admin),
	))
	return &types.MsgRegisterInstitutionResponse{}, nil
}

// RemoveInstitution removes an institution after wind-down (vault_balance must be zero).
func (k msgServer) RemoveInstitution(goCtx context.Context, msg *types.MsgRemoveInstitution) (*types.MsgRemoveInstitutionResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if !k.canManageRegistry(ctx, msg.Operator) {
		return nil, errors.Wrap(types.ErrUnauthorized, "only operator (bootstrap) or governance may remove institutions")
	}
	inst, found := k.GetInstitution(ctx, msg.Id)
	if !found {
		return nil, errors.Wrapf(types.ErrInstitutionNotFound, "id %s", msg.Id)
	}
	if !mustInt(inst.VaultBalance).IsZero() {
		return nil, errors.Wrapf(types.ErrVaultNotEmpty, "vault_balance=%s", inst.VaultBalance)
	}

	k.DeleteInstitution(ctx, msg.Id)
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeInstitutionRemoved,
		sdk.NewAttribute(types.AttributeKeyInstitution, msg.Id),
	))
	return &types.MsgRemoveInstitutionResponse{}, nil
}

// InstitutionMint mints against a confirmed deposit.
// Invariants: signer = institution admin; institution is neither frozen nor paused; new vault_balance
// <= attested_reserve; and the uphi conversion is integral (no remainder at the current rate).
func (k msgServer) InstitutionMint(goCtx context.Context, msg *types.MsgInstitutionMint) (*types.MsgInstitutionMintResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	inst, found := k.GetInstitution(ctx, msg.Institution)
	if !found {
		return nil, errors.Wrapf(types.ErrInstitutionNotFound, "id %s", msg.Institution)
	}
	// Role gate: mint only with the operator or admin role (may be an automated service key).
	if err := k.requireRole(ctx, inst, msg.Admin, types.INSTITUTION_ROLE_OPERATOR, types.INSTITUTION_ROLE_ADMIN); err != nil {
		return nil, err
	}
	// fx provenance: required for an fx institution, forbidden otherwise (metadata only).
	if err := validateFxMetadata(inst.InstitutionType, msg.FxCurrency, msg.FxAmount, msg.FxTxRef); err != nil {
		return nil, err
	}
	// A non-empty deposit_ref is required: the idempotency marker below is keyed on it, so an
	// empty ref would silently disable anti-replay and allow the same deposit to mint twice.
	if msg.DepositRef == "" {
		return nil, errors.Wrap(types.ErrMissingRef, "deposit_ref is required")
	}
	// Idempotency: the same deposit is not minted twice (safe retry for the bank's automated system).
	if k.depositSeen(ctx, inst.Id, "mint", msg.DepositRef) {
		return nil, errors.Wrapf(types.ErrDuplicateDeposit, "deposit_ref=%s", msg.DepositRef)
	}
	if inst.Status == types.INSTITUTION_STATUS_FROZEN {
		return nil, types.ErrInstitutionFrozen
	}
	if inst.PausedMint {
		return nil, types.ErrMintPaused
	}

	toman, ok := math.NewIntFromString(msg.AmountToman)
	if !ok || !toman.IsPositive() {
		return nil, errors.Wrapf(types.ErrInvalidAmount, "amount_toman: %q", msg.AmountToman)
	}

	// Deposit-proof verification: once the institution registers a deposit-signing key, every mint
	// must carry a valid P-256 signature by that key over the canonical deposit message (provably-backed
	// minting). Fail-closed — the default build's verifier rejects, so production must run -tags phicrypto_cgo.
	if len(inst.DepositPubkey) > 0 {
		m := buildDepositMessage(inst.Id, msg.Recipient, msg.AmountToman, msg.DepositRef,
			inst.InstitutionType == types.INSTITUTION_TYPE_FX, msg.FxCurrency, msg.FxAmount, msg.FxTxRef)
		if !k.verifier.VerifySignature(phicrypto.Secp256r1, inst.DepositPubkey, m, msg.DepositProof) {
			return nil, errors.Wrap(types.ErrInvalidDepositProof, "deposit_proof verification failed")
		}
	}

	// Convert toman -> uphi at the parameterized rate (must be integral).
	phiToToman := k.GetParams(ctx).PhiToToman
	uphi, integral := MintedUphiForToman(toman, phiToToman)
	if !integral || !uphi.IsPositive() {
		return nil, errors.Wrapf(types.ErrNonIntegralMint, "amount_toman=%s, phi_to_toman=%d", msg.AmountToman, phiToToman)
	}

	// Per-institution invariant: new vault_balance <= attested reserve.
	newVault := mustInt(inst.VaultBalance).Add(toman)
	if newVault.GT(mustInt(inst.AttestedReserve)) {
		return nil, errors.Wrapf(types.ErrMintExceedsBacking,
			"new vault_balance=%s > attested_reserve=%s", newVault, inst.AttestedReserve)
	}

	recipient, err := sdk.AccAddressFromBech32(msg.Recipient)
	if err != nil {
		return nil, err
	}

	// Institution caps (per-tx / daily / per-user) + KYC-tier daily limit - tighten-only; minting remains backing-constrained.
	if err := k.enforceMintCaps(ctx, inst, recipient, toman, msg.KycTier); err != nil {
		return nil, err
	}

	// Large-mint multisig: a mint at or above the governed threshold must collect the
	// institution's aggregated ADMIN approvals before it executes, so a single operator/admin key
	// cannot mint an unbounded amount. Smaller mints pass straight through. The content hash binds the
	// approval to these exact mint parameters; the deposit_ref idempotency marker is written only on
	// execution below, so re-submitting the same mint accumulates approvals rather than minting twice.
	if lim := types.CapInt(k.GetParams(ctx).LargeMintThresholdToman); lim.IsPositive() && toman.GTE(lim) {
		ch := contentHashOf([]byte("mint"), []byte(inst.Id), []byte(msg.Recipient), []byte(msg.AmountToman),
			[]byte(msg.DepositRef), []byte(msg.FxCurrency), []byte(msg.FxAmount), []byte(msg.FxTxRef))
		r, err := k.trySensitive(ctx, inst, msg.Admin, ch)
		if err != nil {
			return nil, err
		}
		if !r.executed {
			emitPending(ctx, inst.Id, "mint", r)
			return &types.MsgInstitutionMintResponse{MintedUphi: "0"}, nil
		}
		k.clearApprovals(ctx, inst.Id, ch)
	}

	// Mint uphi from the module and send to the recipient.
	coins := cointypes.CoinsOf(uphi)
	if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, coins); err != nil {
		return nil, err
	}
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipient, coins); err != nil {
		return nil, err
	}

	// Record the freshly minted coin age (young from now) - for tiered burn.
	k.coinKeeper.AddYoungCoins(ctx, msg.Recipient, uphi, ctx.BlockTime().Unix())

	// Update vault_balance - the global invariant is preserved (sum(vault)*1e6 = supply*phi_to_toman).
	inst.VaultBalance = newVault.String()
	k.SetInstitution(ctx, inst)

	// Increment the cap counters and record the idempotency marker (deposit_ref is required above).
	k.addMintCounters(ctx, inst, recipient, toman)
	k.markDeposit(ctx, inst.Id, "mint", msg.DepositRef)

	// Enforce the global solvency invariant on the write path (deterministic, every tx) — not only
	// via the periodic x/crisis sweep. Mint raises supply and vault together, so this always holds;
	// the guard fails the tx if any future change ever desynchronizes them.
	if err := k.assertSolvency(ctx); err != nil {
		return nil, err
	}

	mintAttrs := append([]sdk.Attribute{
		sdk.NewAttribute(types.AttributeKeyInstitution, inst.Id),
		sdk.NewAttribute(types.AttributeKeyRecipient, msg.Recipient),
		sdk.NewAttribute(types.AttributeKeyAmountToman, toman.String()),
		sdk.NewAttribute(types.AttributeKeyMintedUphi, uphi.String()),
	}, fxEventAttributes(inst, msg.FxCurrency, msg.FxAmount, msg.FxTxRef)...)
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeInstitutionMinted, mintAttrs...))
	return &types.MsgInstitutionMintResponse{MintedUphi: uphi.String()}, nil
}

// InstitutionRedeem redeems/burns - always open (even when minting is paused or the institution is frozen).
// Invariant: vault_balance >= amount; no inter-institution settlement.
func (k msgServer) InstitutionRedeem(goCtx context.Context, msg *types.MsgInstitutionRedeem) (*types.MsgInstitutionRedeemResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	inst, found := k.GetInstitution(ctx, msg.Institution)
	if !found {
		return nil, errors.Wrapf(types.ErrInstitutionNotFound, "id %s", msg.Institution)
	}
	// Holder consent: redeem burns the holder's own uphi, so it must be authorized BY the
	// holder. The bank keeper would otherwise let the institution operator (the tx signer) pull uphi
	// from ANY account without that holder's signature — an unbounded force-burn primitive. Strict
	// self-redeem: the signer (admin) must be the holder. Institutions observe the on-chain redeem
	// event and settle Toman off-chain. ValidateBasic enforces the same equality at ingress.
	if msg.Admin != msg.Holder {
		return nil, errors.Wrap(types.ErrUnauthorized, "redeem must be signed by the holder (admin must equal holder)")
	}
	// fx provenance: required for an fx institution, forbidden otherwise (metadata only).
	if err := validateFxMetadata(inst.InstitutionType, msg.FxCurrency, msg.FxAmount, msg.FxTxRef); err != nil {
		return nil, err
	}
	// A non-empty redeem_ref is required: the idempotency marker below is keyed on it, so an
	// empty ref would silently disable anti-replay.
	if msg.RedeemRef == "" {
		return nil, errors.Wrap(types.ErrMissingRef, "redeem_ref is required")
	}
	// Idempotency: the same redemption is not burned twice.
	if k.depositSeen(ctx, inst.Id, "redeem", msg.RedeemRef) {
		return nil, errors.Wrapf(types.ErrDuplicateDeposit, "redeem_ref=%s", msg.RedeemRef)
	}

	toman, ok := math.NewIntFromString(msg.AmountToman)
	if !ok || !toman.IsPositive() {
		return nil, errors.Wrapf(types.ErrInvalidAmount, "amount_toman: %q", msg.AmountToman)
	}
	if toman.GT(mustInt(inst.VaultBalance)) {
		return nil, errors.Wrapf(types.ErrInsufficientVault, "amount=%s > vault_balance=%s", toman, inst.VaultBalance)
	}

	phiToToman := k.GetParams(ctx).PhiToToman
	uphi, integral := MintedUphiForToman(toman, phiToToman)
	if !integral || !uphi.IsPositive() {
		return nil, errors.Wrapf(types.ErrNonIntegralMint, "amount_toman=%s, phi_to_toman=%d", msg.AmountToman, phiToToman)
	}

	holder, err := sdk.AccAddressFromBech32(msg.Holder)
	if err != nil {
		return nil, err
	}

	// Redeem caps (per-tx / daily / per-user) + KYC tier + emergency brake - never below the protocol floor.
	if err := k.enforceRedeemCaps(ctx, inst, holder, toman, msg.KycTier); err != nil {
		return nil, err
	}

	// Withdraw uphi from the holder and burn the full amount.
	coins := cointypes.CoinsOf(uphi)
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, holder, types.ModuleName, coins); err != nil {
		return nil, err
	}
	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, coins); err != nil {
		return nil, err
	}

	// Decrease vault_balance by the full amount - the global invariant is preserved (supply and vault drop together).
	inst.VaultBalance = mustInt(inst.VaultBalance).Sub(toman).String()
	k.SetInstitution(ctx, inst)

	// Record the redeem counters so the daily / per-user caps enforced above actually accumulate
	// across transactions (mirrors addMintCounters on the mint path).
	k.addRedeemCounters(ctx, inst, holder, toman)
	// Record the idempotency marker the depositSeen check above reads: without this the
	// redeem_ref check is inert and a redemption could be replayed.
	k.markDeposit(ctx, inst.Id, "redeem", msg.RedeemRef)

	// Enforce the global solvency invariant on the write path (redeem lowers supply and vault
	// together, so it always holds; the guard catches any future desync deterministically).
	if err := k.assertSolvency(ctx); err != nil {
		return nil, err
	}

	// Tiered coin-age exit fee: deducted only from the toman payout (not from phi/vault).
	// feeUphi is based on the seller's coin age; convert to toman: feeUphi * phi_to_toman / UphiPerPhi.
	feeUphi := k.coinKeeper.RedeemDemurrage(ctx, msg.Holder, uphi)
	// The demurrage fee returned by the coin keeper must never be negative nor exceed the amount
	// actually burned.
	if feeUphi.IsNegative() {
		feeUphi = math.ZeroInt()
	}
	if feeUphi.GT(uphi) {
		feeUphi = uphi
	}
	// Overflow-safe conversion uphi -> toman: feeUphi * phi_to_toman / UphiPerPhi.
	// (phi_to_toman is a uint64 param; using NewIntFromUint64 avoids the int64 cast overflow.)
	feeToman := feeUphi.Mul(math.NewIntFromUint64(phiToToman)).Quo(math.NewInt(cointypes.UphiPerPhi))
	// Clamp so the off-chain toman payout can never go negative.
	if feeToman.GT(toman) {
		feeToman = toman
	}
	payoutToman := toman.Sub(feeToman)

	redeemAttrs := append([]sdk.Attribute{
		sdk.NewAttribute(types.AttributeKeyInstitution, inst.Id),
		sdk.NewAttribute(types.AttributeKeyHolder, msg.Holder),
		sdk.NewAttribute(types.AttributeKeyAmountToman, toman.String()),
		sdk.NewAttribute(types.AttributeKeyBurnedUphi, uphi.String()),
		sdk.NewAttribute(types.AttributeKeyFeeToman, feeToman.String()),
		sdk.NewAttribute(types.AttributeKeyPayoutToman, payoutToman.String()),
	}, fxEventAttributes(inst, msg.FxCurrency, msg.FxAmount, msg.FxTxRef)...)
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeInstitutionRedeemed, redeemAttrs...))
	return &types.MsgInstitutionRedeemResponse{BurnedUphi: uphi.String()}, nil
}

// PublishInstitutionAttestation updates the vault's attested reserve.
func (k msgServer) PublishInstitutionAttestation(goCtx context.Context, msg *types.MsgPublishInstitutionAttestation) (*types.MsgPublishInstitutionAttestationResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	inst, found := k.GetInstitution(ctx, msg.Institution)
	if !found {
		return nil, errors.Wrapf(types.ErrInstitutionNotFound, "id %s", msg.Institution)
	}
	// Role gate: publishing an attestation requires the COMPLIANCE role (or the ADMIN
	// root) — NOT the OPERATOR that mints — so a single operator key cannot both self-attest a reserve
	// and mint against it (separation of duties).
	if err := k.requireRole(ctx, inst, msg.Admin, types.INSTITUTION_ROLE_COMPLIANCE, types.INSTITUTION_ROLE_ADMIN); err != nil {
		return nil, err
	}
	reserve, ok := math.NewIntFromString(msg.AttestedReserve)
	if !ok || reserve.IsNegative() {
		return nil, errors.Wrapf(types.ErrInvalidAmount, "attested_reserve: %q", msg.AttestedReserve)
	}

	inst.AttestedReserve = reserve.String()
	// Health status: if the reserve drops below minted phi -> low liquidity (until phi-bridge auto-freeze).
	if !inst.PausedMint && inst.Status != types.INSTITUTION_STATUS_FROZEN {
		if reserve.LT(mustInt(inst.VaultBalance)) {
			inst.Status = types.INSTITUTION_STATUS_LOW_LIQ
		} else {
			inst.Status = types.INSTITUTION_STATUS_HEALTHY
		}
	}
	k.SetInstitution(ctx, inst)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeAttestationPublished,
		sdk.NewAttribute(types.AttributeKeyInstitution, inst.Id),
		sdk.NewAttribute(types.AttributeKeyAttestedReserve, reserve.String()),
	))
	// A reserve below the minted vault is an allowed LOW_LIQ state, not a consensus halt — surface
	// it as a health metric/event instead of (formerly) a registered, chain-halting invariant.
	k.EmitBackingHealth(ctx, inst)
	return &types.MsgPublishInstitutionAttestationResponse{}, nil
}

// FreezeInstitution pauses or resumes an institution's minting.
func (k msgServer) FreezeInstitution(goCtx context.Context, msg *types.MsgFreezeInstitution) (*types.MsgFreezeInstitutionResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if !k.canManageRegistry(ctx, msg.Operator) {
		return nil, errors.Wrap(types.ErrUnauthorized, "only operator (bootstrap) or governance may freeze institutions")
	}
	inst, found := k.GetInstitution(ctx, msg.Id)
	if !found {
		return nil, errors.Wrapf(types.ErrInstitutionNotFound, "id %s", msg.Id)
	}

	if msg.Frozen {
		inst.Status = types.INSTITUTION_STATUS_FROZEN
	} else {
		inst.Status = types.INSTITUTION_STATUS_HEALTHY
	}
	k.SetInstitution(ctx, inst)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeInstitutionFrozen,
		sdk.NewAttribute(types.AttributeKeyInstitution, inst.Id),
		sdk.NewAttribute(types.AttributeKeyFrozen, boolToStr(msg.Frozen)),
	))
	return &types.MsgFreezeInstitutionResponse{}, nil
}

// UpdateParams - governance authority only.
func (k msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if msg.Authority != k.authority {
		return nil, errors.Wrapf(govtypes.ErrInvalidSigner, "expected %s, got %s", k.authority, msg.Authority)
	}
	// The penalty destination must be sendable, or routing slashed-stake compensation to it would fail
	// in the slashing BeginBlocker. Reject a blocked address at param-set time.
	if msg.Params.PenaltyDestination != "" {
		dest, err := sdk.AccAddressFromBech32(msg.Params.PenaltyDestination)
		if err != nil {
			return nil, errors.Wrapf(types.ErrInvalidParams, "penalty_destination: %v", err)
		}
		if k.bankKeeper.BlockedAddr(dest) {
			return nil, errors.Wrap(types.ErrInvalidParams, "penalty_destination is a blocked address")
		}
	}
	if err := k.SetParams(ctx, msg.Params); err != nil {
		return nil, err
	}
	return &types.MsgUpdateParamsResponse{}, nil
}

// validateFxMetadata enforces that the fx provenance fields are present for an fx institution and
// absent for any other type. These fields are provenance metadata only - they never affect the
// vault (always Rial) or the global solvency invariant.
func validateFxMetadata(instType types.InstitutionType, currency, amount, txRef string) error {
	isFx := instType == types.INSTITUTION_TYPE_FX
	hasAll := currency != "" && amount != "" && txRef != ""
	hasAny := currency != "" || amount != "" || txRef != ""
	if isFx && !hasAll {
		return errors.Wrap(types.ErrInvalidFxMetadata, "fx institution requires fx_currency, fx_amount and fx_tx_ref")
	}
	if !isFx && hasAny {
		return errors.Wrap(types.ErrInvalidFxMetadata, "fx_* metadata is only allowed for fx institutions")
	}
	return nil
}

// fxEventAttributes returns the provenance attributes for a mint/redeem event: always the
// institution type, plus the fx fields when the institution is fx.
func fxEventAttributes(inst types.Institution, currency, amount, txRef string) []sdk.Attribute {
	attrs := []sdk.Attribute{sdk.NewAttribute(types.AttributeKeyInstitutionType, inst.InstitutionType.String())}
	if inst.InstitutionType == types.INSTITUTION_TYPE_FX {
		attrs = append(attrs,
			sdk.NewAttribute(types.AttributeKeyFxCurrency, currency),
			sdk.NewAttribute(types.AttributeKeyFxAmount, amount),
			sdk.NewAttribute(types.AttributeKeyFxTxRef, txRef),
		)
	}
	return attrs
}

func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// depositMessageDomain is the canonical deposit-attestation domain (see docs/institutions-deposit-proof.md).
const depositMessageDomain = "phi-deposit-attestation-v1"

// buildDepositMessage constructs the canonical message the institution's deposit key signs:
//
//	domain ‖ 0x00 ‖ institution ‖ 0x00 ‖ recipient ‖ 0x00 ‖ amount_toman ‖ 0x00 ‖ deposit_ref
//	[ ‖ 0x00 ‖ fx_currency ‖ 0x00 ‖ fx_amount ‖ 0x00 ‖ fx_tx_ref ]   (fx institutions only)
//
// The fields are text (id/bech32/decimal/refs), so the 0x00 separator is unambiguous.
func buildDepositMessage(instID, recipient, amountToman, depositRef string, isFx bool, fxCurrency, fxAmount, fxTxRef string) []byte {
	parts := [][]byte{
		[]byte(depositMessageDomain),
		[]byte(instID), []byte(recipient), []byte(amountToman), []byte(depositRef),
	}
	if isFx {
		parts = append(parts, []byte(fxCurrency), []byte(fxAmount), []byte(fxTxRef))
	}
	return bytes.Join(parts, []byte{0x00})
}
