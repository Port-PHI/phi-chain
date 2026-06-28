// SPDX-License-Identifier: Apache-2.0

package types

import "fmt"

// DefaultMaxAgreementSigners bounds the required-signer set of an agreement.
const DefaultMaxAgreementSigners = uint32(100)

// DefaultParams returns the default module parameters.
func DefaultParams() Params {
	return Params{
		MaxAgreementSigners: DefaultMaxAgreementSigners,
	}
}

// Validate checks the parameters.
func (p Params) Validate() error {
	if p.MaxAgreementSigners == 0 {
		return fmt.Errorf("max_agreement_signers must be > 0")
	}
	return nil
}
