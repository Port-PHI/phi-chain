// SPDX-License-Identifier: Apache-2.0

package types

import (
	"fmt"
	"strings"

	"cosmossdk.io/math"
)

// Default parameter values.
const (
	// DefaultMinIdentityAgeSeconds is the minimum DID age to qualify for public voting — 7 days.
	DefaultMinIdentityAgeSeconds = int64(7 * 24 * 60 * 60)
	// DefaultBootstrapThreshold is the bootstrap lock threshold for institution onboarding governance.
	DefaultBootstrapThreshold = uint64(10_000)
	// DefaultWebAuthnRPID is the genesis relying-party id bound into a passkey assertion.
	DefaultWebAuthnRPID = "portphi.com"
	// DefaultMaxGuardians bounds a DID's guardian-set size (state-bloat guard).
	DefaultMaxGuardians = uint32(10)
	// DefaultUVLargeTransferUphi is the transfer amount (uphi) at or above which a coin transfer is "sensitive" and a passkey signer must complete a User-Verification gesture — 100 PHI.
	DefaultUVLargeTransferUphi = "100000000"
	// DefaultRecoveryDelaySeconds is the social-recovery opposition window — 72 hours.
	DefaultRecoveryDelaySeconds = int64(72 * 60 * 60)
	// DefaultRecoveryRequestTTLSeconds is how long a recovery request stays alive before it is treated as EXPIRED and its slot is reclaimed — 14 days.
	DefaultRecoveryRequestTTLSeconds = int64(14 * 24 * 60 * 60)
	// DefaultMaxOpenRecoveryRequests caps concurrent PENDING recovery requests per DID.
	DefaultMaxOpenRecoveryRequests = uint32(5)
	// MaxOpenRecoveryRequestsCeiling is the highest value governance may set for the per-DID open -request cap.
	MaxOpenRecoveryRequestsCeiling = uint32(64)
	// DefaultRecoveryDepositUphi is the deposit escrowed at initiate — 1 PHI.
	DefaultRecoveryDepositUphi = "1000000"
	// DefaultReauthRecoveryFeeUphi is the fee charged for a REAUTH recovery — 4 PHI (Tokenomics §4).
	DefaultReauthRecoveryFeeUphi = "4000000"

	// MaxWebAuthnAllowedOrigins caps the governed WebAuthn origin allow-list.
	MaxWebAuthnAllowedOrigins = 32
	// MaxUVSensitiveMsgTypeURLs caps the governed UV-sensitive message-type list, scanned per message to decide whether a passkey signer must complete a User-Verification gesture.
	MaxUVSensitiveMsgTypeURLs = 32
)

// DefaultUVSensitiveMsgTypeURLs is the genesis stepped-UV policy: message types whose presence makes a transaction sensitive, so a passkey signer must complete a User-Verification gesture (biometric/PIN) rather than a mere presence tap.
var DefaultUVSensitiveMsgTypeURLs = []string{
	"/phi.identity.MsgRotateIdentityKey",
	"/phi.identity.MsgSetGuardians",
	"/phi.identity.MsgInitiateRecovery",
	"/phi.identity.MsgApproveRecovery",
	// Rejecting is as consequential as approving and strictly cheaper to forge if left unprotected: a rejection at threshold is immediately terminal and forfeits the initiator's deposit, where an approval only unlocks a later execute that the owner can still oppose.
	"/phi.identity.MsgRejectRecovery",
	"/phi.identity.MsgCancelRecovery",
	"/phi.institutions.MsgInstitutionRedeem",
}

// DefaultWebAuthnAllowedOrigins is the genesis allow-list of WebAuthn origins (anti-phishing).
var DefaultWebAuthnAllowedOrigins = []string{"https://portphi.com"}

// DefaultParams returns the module's default parameters.
func DefaultParams() Params {
	return Params{
		MinIdentityAgeSeconds:     DefaultMinIdentityAgeSeconds,
		BootstrapThreshold:        DefaultBootstrapThreshold,
		BootstrapPhase:            true,
		WebauthnAllowedOrigins:    DefaultWebAuthnAllowedOrigins,
		WebauthnRpId:              DefaultWebAuthnRPID,
		MaxGuardians:              DefaultMaxGuardians,
		UvSensitiveMsgTypeUrls:    DefaultUVSensitiveMsgTypeURLs,
		UvLargeTransferUphi:       DefaultUVLargeTransferUphi,
		RecoveryDelaySeconds:      DefaultRecoveryDelaySeconds,
		RecoveryRequestTtlSeconds: DefaultRecoveryRequestTTLSeconds,
		MaxOpenRecoveryRequests:   DefaultMaxOpenRecoveryRequests,
		RecoveryDepositUphi:       DefaultRecoveryDepositUphi,
		ReauthRecoveryFeeUphi:     DefaultReauthRecoveryFeeUphi,
	}
}

// Validate checks the parameters for correctness.
func (p Params) Validate() error {
	if p.MinIdentityAgeSeconds < 0 {
		return fmt.Errorf("min_identity_age_seconds must be >= 0: %d", p.MinIdentityAgeSeconds)
	}
	if p.BootstrapThreshold == 0 {
		return fmt.Errorf("bootstrap_threshold must be > 0")
	}
	// WebAuthn relying-party config: require a non-empty allow-list and rpId so governance cannot set an empty/permissive anti-phishing binding for the (gated) on-chain passkey verifier.
	if len(p.WebauthnAllowedOrigins) == 0 {
		return fmt.Errorf("webauthn_allowed_origins must list at least one origin")
	}
	if len(p.WebauthnAllowedOrigins) > MaxWebAuthnAllowedOrigins {
		return fmt.Errorf("webauthn_allowed_origins has %d entries (max %d): bounds per-signature verify work",
			len(p.WebauthnAllowedOrigins), MaxWebAuthnAllowedOrigins)
	}
	for i, o := range p.WebauthnAllowedOrigins {
		if o == "" {
			return fmt.Errorf("webauthn_allowed_origins[%d] must not be empty", i)
		}
	}
	if p.WebauthnRpId == "" {
		return fmt.Errorf("webauthn_rp_id must not be empty")
	}
	// A zero cap would make every guardian set unsettable (len(guardians) >= 1 is required).
	if p.MaxGuardians == 0 {
		return fmt.Errorf("max_guardians must be > 0")
	}
	// Stepped-UV policy: every entry must be a well-formed message type URL, and the large-transfer threshold must be a non-negative integer amount ("0" disables the amount rule).
	if len(p.UvSensitiveMsgTypeUrls) > MaxUVSensitiveMsgTypeURLs {
		return fmt.Errorf("uv_sensitive_msg_type_urls has %d entries (max %d)",
			len(p.UvSensitiveMsgTypeUrls), MaxUVSensitiveMsgTypeURLs)
	}
	for i, u := range p.UvSensitiveMsgTypeUrls {
		if u == "" || !strings.HasPrefix(u, "/") {
			return fmt.Errorf("uv_sensitive_msg_type_urls[%d] must be a message type URL starting with '/': %q", i, u)
		}
	}
	amt, ok := math.NewIntFromString(p.UvLargeTransferUphi)
	if !ok {
		return fmt.Errorf("invalid uv_large_transfer_uphi: %q", p.UvLargeTransferUphi)
	}
	if amt.IsNegative() {
		return fmt.Errorf("uv_large_transfer_uphi must be >= 0, got %s", p.UvLargeTransferUphi)
	}

	// Social recovery.
	if p.RecoveryDelaySeconds <= 0 {
		// A zero window would let a hijack execute in the same block it was initiated — the opposition period is the whole anti-hijack defence.
		return fmt.Errorf("recovery_delay_seconds must be > 0, got %d", p.RecoveryDelaySeconds)
	}
	if p.RecoveryRequestTtlSeconds <= p.RecoveryDelaySeconds {
		// A TTL at or below the window would expire every request before it could ever execute.
		return fmt.Errorf("recovery_request_ttl_seconds (%d) must exceed recovery_delay_seconds (%d)",
			p.RecoveryRequestTtlSeconds, p.RecoveryDelaySeconds)
	}
	if p.MaxOpenRecoveryRequests == 0 {
		return fmt.Errorf("max_open_recovery_requests must be > 0")
	}
	if p.MaxOpenRecoveryRequests > MaxOpenRecoveryRequestsCeiling {
		return fmt.Errorf("max_open_recovery_requests is %d (max %d): it bounds the per-DID scans run inside recovery handlers",
			p.MaxOpenRecoveryRequests, MaxOpenRecoveryRequestsCeiling)
	}
	dep, ok := math.NewIntFromString(p.RecoveryDepositUphi)
	if !ok {
		return fmt.Errorf("invalid recovery_deposit_uphi: %q", p.RecoveryDepositUphi)
	}
	if dep.IsNegative() {
		return fmt.Errorf("recovery_deposit_uphi must be >= 0, got %s", p.RecoveryDepositUphi)
	}
	fee, ok := math.NewIntFromString(p.ReauthRecoveryFeeUphi)
	if !ok {
		return fmt.Errorf("invalid reauth_recovery_fee_uphi: %q", p.ReauthRecoveryFeeUphi)
	}
	if fee.IsNegative() {
		return fmt.Errorf("reauth_recovery_fee_uphi must be >= 0, got %s", p.ReauthRecoveryFeeUphi)
	}
	return nil
}

// RecoveryDeposit returns the parsed recovery deposit (uphi).
func (p Params) RecoveryDeposit() math.Int {
	dep, ok := math.NewIntFromString(p.RecoveryDepositUphi)
	if !ok {
		return math.ZeroInt()
	}
	return dep
}

// ReauthRecoveryFee returns the parsed REAUTH recovery fee (uphi).
func (p Params) ReauthRecoveryFee() math.Int {
	fee, ok := math.NewIntFromString(p.ReauthRecoveryFeeUphi)
	if !ok {
		return math.ZeroInt()
	}
	return fee
}

// UVLargeTransferAmount returns the parsed large-transfer threshold (uphi).
func (p Params) UVLargeTransferAmount() math.Int {
	amt, ok := math.NewIntFromString(p.UvLargeTransferUphi)
	if !ok {
		return math.ZeroInt()
	}
	return amt
}
