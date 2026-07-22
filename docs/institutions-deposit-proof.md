<!-- SPDX-License-Identifier: Apache-2.0 -->
# Institution deposit-proof verification

`MsgInstitutionMint.deposit_proof` is a signed attestation that the institution received
the Rial deposit it is minting against. Verifying it on-chain against a key the institution
registered makes minting provably backed, rather than resting only on role plus attested
reserve headroom.

## Deposit-signing key

`Institution.deposit_pubkey` (P-256 SEC1) is the key the institution's vault system uses to
sign deposit attestations. It is set through the sensitive multisig message
`MsgSetInstitutionDepositKey` (the same aggregated-approval path as the other sensitive actions).

## Canonical deposit message

```
msg = "phi-deposit-attestation-v1" || 0x00 ||
      institution_id || 0x00 || recipient || 0x00 || amount_toman || 0x00 || deposit_ref
```

For an fx institution the fx provenance is appended:
`|| 0x00 || fx_currency || 0x00 || fx_amount || 0x00 || fx_tx_ref`.

## Handler check (InstitutionMint)

Verification is gated on a configured key, so an institution that has not registered one is
unaffected; once a key is set, a valid proof is required for every mint:

```go
if len(inst.DepositPubkey) > 0 {
    m := buildDepositMessage(inst.Id, msg.Recipient, msg.AmountToman, msg.DepositRef,
        inst.InstitutionType == types.INSTITUTION_TYPE_FX, msg.FxCurrency, msg.FxAmount, msg.FxTxRef)
    if !k.verifier.VerifySignature(phicrypto.Secp256r1, inst.DepositPubkey, m, msg.DepositProof) {
        return nil, errors.Wrap(types.ErrInvalidDepositProof, "deposit_proof verification failed")
    }
}
```

The check runs through the `phicrypto.Verifier` port and is fail-closed (the default
build's verifier rejects; real verification under `-tags phicrypto_cgo`).

## KYC-tier limits

`MsgInstitutionMint`/`MsgInstitutionRedeem` carry `kyc_tier`, asserted by the institution
off-chain. When `InstitutionParams.kyc_tier_limits` has a matching tier, its
`daily_limit_toman` is enforced in the day-bucketed per-user accounting in `enforceMintCaps`
/ `enforceRedeemCaps`.
