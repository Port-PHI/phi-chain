// SPDX-License-Identifier: Apache-2.0

package ante

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"slices"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/Port-PHI/phi-chain/phicrypto"
	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
)

// On-chain WebAuthn verification core (consensus-critical): a passkey signs over authData‖SHA256(clientDataJSON), not the tx sign-bytes, so signature checking is delegated to phi-crypto; without phicrypto_cgo every envelope fails closed.

var webAuthnMagic = []byte("PWA1")

// WebAuthnSignature is the wire envelope carried in a signer's signature bytes when authenticating with a passkey.
type WebAuthnSignature struct {
	AuthenticatorData []byte
	ClientDataJSON    []byte
	Signature         []byte // raw ECDSA P-256 r‖s (or DER) over authData ‖ SHA256(clientDataJSON)
}

// Marshal encodes magic ‖ u32-BE-len ‖ field, for authData, clientDataJSON, signature.
func (w WebAuthnSignature) Marshal() []byte {
	out := make([]byte, 0, len(webAuthnMagic)+12+len(w.AuthenticatorData)+len(w.ClientDataJSON)+len(w.Signature))
	out = append(out, webAuthnMagic...)
	for _, field := range [][]byte{w.AuthenticatorData, w.ClientDataJSON, w.Signature} {
		var lenBuf [4]byte
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(field)))
		out = append(out, lenBuf[:]...)
		out = append(out, field...)
	}
	return out
}

// IsWebAuthnEnvelope reports whether sig carries a WebAuthn envelope (magic prefix).
func IsWebAuthnEnvelope(sig []byte) bool {
	return len(sig) >= len(webAuthnMagic) && bytes.Equal(sig[:len(webAuthnMagic)], webAuthnMagic)
}

// UnmarshalWebAuthnSignature strictly decodes a Marshal envelope (rejects missing magic, truncation, trailing bytes).
func UnmarshalWebAuthnSignature(b []byte) (WebAuthnSignature, error) {
	if !IsWebAuthnEnvelope(b) {
		return WebAuthnSignature{}, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "webauthn: missing envelope magic")
	}
	rest := b[len(webAuthnMagic):]
	fields := make([][]byte, 3)
	for i := range fields {
		if len(rest) < 4 {
			return WebAuthnSignature{}, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "webauthn: truncated length prefix")
		}
		n := binary.BigEndian.Uint32(rest[:4])
		rest = rest[4:]
		if uint64(len(rest)) < uint64(n) {
			return WebAuthnSignature{}, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "webauthn: field %d truncated", i)
		}
		fields[i] = rest[:n]
		rest = rest[n:]
	}
	if len(rest) != 0 {
		return WebAuthnSignature{}, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "webauthn: trailing bytes after envelope")
	}
	return WebAuthnSignature{AuthenticatorData: fields[0], ClientDataJSON: fields[1], Signature: fields[2]}, nil
}

var webAuthnChallengeDomain = []byte("PHI-WEBAUTHN-v1")

// WebAuthnChallenge derives the tx-bound challenge SHA256("PHI-WEBAUTHN-v1" ‖ signBytes) the client embeds as clientDataJSON.challenge.
func WebAuthnChallenge(signBytes []byte) []byte {
	h := sha256.New()
	h.Write(webAuthnChallengeDomain)
	h.Write(signBytes)
	return h.Sum(nil)
}

// VerifyWebAuthnAssertion verifies a passkey assertion via phi-crypto (all crypto checks inside; fail-safe under Disabled).
func VerifyWebAuthnAssertion(verifier phicrypto.Verifier, env WebAuthnSignature, publicKey, signBytes []byte, origin, rpID string, requireUV bool) error {
	assertion := phicrypto.WebAuthnAssertion{
		AuthenticatorData:       env.AuthenticatorData,
		ClientDataJSON:          env.ClientDataJSON,
		Signature:               env.Signature,
		Challenge:               WebAuthnChallenge(signBytes),
		PublicKey:               publicKey,
		Origin:                  origin,
		RPID:                    rpID,
		RequireUserVerification: requireUV,
	}
	if !verifier.VerifyWebAuthn(assertion) {
		return errorsmod.Wrap(sdkerrors.ErrUnauthorized, "webauthn assertion verification failed")
	}
	return nil
}

// WebAuthnParamSource provides the governed relying-party config (allowed-origin list + rpId) from state.
type WebAuthnParamSource interface {
	WebAuthnRelyingParty(ctx sdk.Context) (allowedOrigins []string, rpID string)
}

// UVPolicySource provides the governed stepped-UV policy: sensitive msg type URLs + the transfer amount making a transfer sensitive (0 disables).
type UVPolicySource interface {
	UVPolicy(ctx sdk.Context) (sensitiveMsgTypeURLs []string, largeTransferUphi math.Int)
}

// WebAuthnDecorator is the consensus-critical WebAuthn routing helper embedded by PhiSigVerificationDecorator; envelopes fail closed without the cgo link.
type WebAuthnDecorator struct {
	verifier phicrypto.Verifier
	params   WebAuthnParamSource
	uvPolicy UVPolicySource
}

// NewWebAuthnDecorator binds the decorator to a verifier (nil → fail-safe phicrypto.Default()) and the governed sources.
func NewWebAuthnDecorator(verifier phicrypto.Verifier, params WebAuthnParamSource, uvPolicy UVPolicySource) WebAuthnDecorator {
	if verifier == nil {
		verifier = phicrypto.Default()
	}
	return WebAuthnDecorator{verifier: verifier, params: params, uvPolicy: uvPolicy}
}

// RequiresUserVerification reports whether the tx is sensitive under the governed stepped-UV policy (per-tx: any sensitive msg raises the bar for all signers).
func (d WebAuthnDecorator) RequiresUserVerification(ctx sdk.Context, msgs []sdk.Msg) bool {
	sensitive, largeTransfer := d.uvPolicy.UVPolicy(ctx)
	set := make(map[string]struct{}, len(sensitive))
	for _, u := range sensitive {
		set[u] = struct{}{}
	}
	for _, m := range msgs {
		if _, ok := set[sdk.MsgTypeURL(m)]; ok {
			return true
		}
		// Amount-based rule: a large transfer is sensitive even though MsgTransfer itself is not.
		if t, ok := m.(*cointypes.MsgTransfer); ok && largeTransfer.IsPositive() {
			// A malformed amount fails closed (treated as sensitive).
			amt, ok := math.NewIntFromString(t.Amount)
			if !ok || amt.GTE(largeTransfer) {
				return true
			}
		}
	}
	return false
}

// VerifyEnvelope is the per-signer routing decision: not-an-envelope→(false,nil); envelope→(true, verify result), fail-closed.
func (d WebAuthnDecorator) VerifyEnvelope(ctx sdk.Context, pubKey, signBytes, sig []byte, requireUV bool) (handled bool, err error) {
	if !IsWebAuthnEnvelope(sig) {
		return false, nil
	}
	env, err := UnmarshalWebAuthnSignature(sig)
	if err != nil {
		return true, err
	}
	allowedOrigins, rpID := d.params.WebAuthnRelyingParty(ctx)

	origin, err := clientDataOrigin(env.ClientDataJSON)
	if err != nil {
		return true, err
	}
	if !slices.Contains(allowedOrigins, origin) {
		// Origin not echoed back: attacker-controlled text.
		return true, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "webauthn assertion origin is not on the allowed list")
	}

	if VerifyWebAuthnAssertion(d.verifier, env, pubKey, signBytes, origin, rpID, requireUV) != nil {
		if requireUV {
			return true, errorsmod.Wrap(sdkerrors.ErrUnauthorized,
				"webauthn assertion did not verify (this transaction is sensitive and requires User-Verification)")
		}
		return true, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "webauthn assertion did not verify")
	}
	return true, nil
}

func clientDataOrigin(clientDataJSON []byte) (string, error) {
	var cd struct {
		Origin string `json:"origin"`
	}
	if err := json.Unmarshal(clientDataJSON, &cd); err != nil {
		return "", errorsmod.Wrap(sdkerrors.ErrUnauthorized, "webauthn client data is not valid JSON")
	}
	if cd.Origin == "" {
		return "", errorsmod.Wrap(sdkerrors.ErrUnauthorized, "webauthn client data carries no origin")
	}
	return cd.Origin, nil
}
