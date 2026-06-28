// SPDX-License-Identifier: Apache-2.0

package types

import "fmt"

// Default parameter values.
const (
	// DefaultMinIdentityAgeSeconds is the minimum DID age to qualify for public voting — 7 days.
	DefaultMinIdentityAgeSeconds = int64(7 * 24 * 60 * 60)
	// DefaultBootstrapThreshold is the bootstrap lock threshold for institution onboarding governance.
	DefaultBootstrapThreshold = uint64(10_000)
	// DefaultWebAuthnRPID is the genesis relying-party id bound into a passkey assertion.
	DefaultWebAuthnRPID = "portphi.com"
)

// DefaultWebAuthnAllowedOrigins is the genesis allow-list of WebAuthn origins (anti-phishing). Further
// origins (e.g. a native app) are added via governance; the binding is consensus-relevant.
var DefaultWebAuthnAllowedOrigins = []string{"https://portphi.com"}

// DefaultParams returns the module's default parameters.
func DefaultParams() Params {
	return Params{
		MinIdentityAgeSeconds:  DefaultMinIdentityAgeSeconds,
		BootstrapThreshold:     DefaultBootstrapThreshold,
		BootstrapPhase:         true,
		WebauthnAllowedOrigins: DefaultWebAuthnAllowedOrigins,
		WebauthnRpId:           DefaultWebAuthnRPID,
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
	// WebAuthn relying-party config: require a non-empty allow-list and rpId so governance
	// cannot set an empty/permissive anti-phishing binding for the (gated) on-chain passkey verifier.
	if len(p.WebauthnAllowedOrigins) == 0 {
		return fmt.Errorf("webauthn_allowed_origins must list at least one origin")
	}
	for i, o := range p.WebauthnAllowedOrigins {
		if o == "" {
			return fmt.Errorf("webauthn_allowed_origins[%d] must not be empty", i)
		}
	}
	if p.WebauthnRpId == "" {
		return fmt.Errorf("webauthn_rp_id must not be empty")
	}
	return nil
}
