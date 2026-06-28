// SPDX-License-Identifier: Apache-2.0

package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers the amino codec.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgRegisterInstitution{}, "phi/institutions/MsgRegisterInstitution", nil)
	cdc.RegisterConcrete(&MsgRemoveInstitution{}, "phi/institutions/MsgRemoveInstitution", nil)
	cdc.RegisterConcrete(&MsgInstitutionMint{}, "phi/institutions/MsgInstitutionMint", nil)
	cdc.RegisterConcrete(&MsgInstitutionRedeem{}, "phi/institutions/MsgInstitutionRedeem", nil)
	cdc.RegisterConcrete(&MsgPublishInstitutionAttestation{}, "phi/institutions/MsgPublishInstitutionAttestation", nil)
	cdc.RegisterConcrete(&MsgFreezeInstitution{}, "phi/institutions/MsgFreezeInstitution", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "phi/institutions/MsgUpdateParams", nil)
	// sensitive actions (RBAC)
	cdc.RegisterConcrete(&MsgGrantInstitutionRole{}, "phi/institutions/MsgGrantInstitutionRole", nil)
	cdc.RegisterConcrete(&MsgRevokeInstitutionRole{}, "phi/institutions/MsgRevokeInstitutionRole", nil)
	cdc.RegisterConcrete(&MsgUpdateInstitutionAppConfig{}, "phi/institutions/MsgUpdateInstitutionAppConfig", nil)
	cdc.RegisterConcrete(&MsgUpdateInstitutionParams{}, "phi/institutions/MsgUpdateInstitutionParams", nil)
	cdc.RegisterConcrete(&MsgSetInstitutionDepositKey{}, "phi/institutions/MsgSetInstitutionDepositKey", nil)
	cdc.RegisterConcrete(&MsgSetEmergencyRedemption{}, "phi/institutions/MsgSetEmergencyRedemption", nil)
	// fx onboarding
	cdc.RegisterConcrete(&MsgRequestFxEntry{}, "phi/institutions/MsgRequestFxEntry", nil)
	cdc.RegisterConcrete(&MsgGuaranteeFxEntry{}, "phi/institutions/MsgGuaranteeFxEntry", nil)
	cdc.RegisterConcrete(&MsgFinalizeFxEntry{}, "phi/institutions/MsgFinalizeFxEntry", nil)
}

// RegisterInterfaces registers the sdk.Msg implementations.
func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterInstitution{},
		&MsgRemoveInstitution{},
		&MsgInstitutionMint{},
		&MsgInstitutionRedeem{},
		&MsgPublishInstitutionAttestation{},
		&MsgFreezeInstitution{},
		&MsgUpdateParams{},
		// sensitive actions (RBAC)
		&MsgGrantInstitutionRole{},
		&MsgRevokeInstitutionRole{},
		&MsgUpdateInstitutionAppConfig{},
		&MsgUpdateInstitutionParams{},
		&MsgSetInstitutionDepositKey{},
		&MsgSetEmergencyRedemption{},
		// fx onboarding
		&MsgRequestFxEntry{},
		&MsgGuaranteeFxEntry{},
		&MsgFinalizeFxEntry{},
	)
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
