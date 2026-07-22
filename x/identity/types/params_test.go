// SPDX-License-Identifier: Apache-2.0

package types

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The governed WebAuthn origin allow-list and UV-sensitive message-type list are length-capped so a governance change cannot arm per-signature verify-work amplification.
func TestParamsValidate_ListLengthCaps(t *testing.T) {
	require.NoError(t, DefaultParams().Validate(), "the default params must validate")

	overOrigins := DefaultParams()
	overOrigins.WebauthnAllowedOrigins = make([]string, MaxWebAuthnAllowedOrigins+1)
	for i := range overOrigins.WebauthnAllowedOrigins {
		overOrigins.WebauthnAllowedOrigins[i] = "https://origin-" + strings.Repeat("x", i+1) + ".example"
	}
	require.Error(t, overOrigins.Validate(), "an over-long webauthn_allowed_origins must be rejected")

	atOrigins := DefaultParams()
	atOrigins.WebauthnAllowedOrigins = make([]string, MaxWebAuthnAllowedOrigins)
	for i := range atOrigins.WebauthnAllowedOrigins {
		atOrigins.WebauthnAllowedOrigins[i] = "https://origin-" + strings.Repeat("x", i+1) + ".example"
	}
	require.NoError(t, atOrigins.Validate(), "a list at the cap must be accepted")

	overUV := DefaultParams()
	overUV.UvSensitiveMsgTypeUrls = make([]string, MaxUVSensitiveMsgTypeURLs+1)
	for i := range overUV.UvSensitiveMsgTypeUrls {
		overUV.UvSensitiveMsgTypeUrls[i] = "/phi.coin.MsgTransfer" + strings.Repeat("x", i)
	}
	require.Error(t, overUV.Validate(), "an over-long uv_sensitive_msg_type_urls must be rejected")
}
