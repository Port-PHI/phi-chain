// SPDX-License-Identifier: Apache-2.0

package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgRegisterIdentity{}, "phi/identity/MsgRegisterIdentity", nil)
	cdc.RegisterConcrete(&MsgRevokeIdentity{}, "phi/identity/MsgRevokeIdentity", nil)
	cdc.RegisterConcrete(&MsgRotateIdentityKey{}, "phi/identity/MsgRotateIdentityKey", nil)
	cdc.RegisterConcrete(&MsgUpdateStatus{}, "phi/identity/MsgUpdateStatus", nil)
	cdc.RegisterConcrete(&MsgSetGuardians{}, "phi/identity/MsgSetGuardians", nil)
	cdc.RegisterConcrete(&MsgInitiateRecovery{}, "phi/identity/MsgInitiateRecovery", nil)
	cdc.RegisterConcrete(&MsgApproveRecovery{}, "phi/identity/MsgApproveRecovery", nil)
	cdc.RegisterConcrete(&MsgRejectRecovery{}, "phi/identity/MsgRejectRecovery", nil)
	cdc.RegisterConcrete(&MsgExecuteRecovery{}, "phi/identity/MsgExecuteRecovery", nil)
	cdc.RegisterConcrete(&MsgCancelRecovery{}, "phi/identity/MsgCancelRecovery", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "phi/identity/MsgUpdateParams", nil)
	cdc.RegisterConcrete(&MsgRegisterTrustedIssuer{}, "phi/identity/MsgRegisterTrustedIssuer", nil)
	cdc.RegisterConcrete(&MsgRevokeTrustedIssuer{}, "phi/identity/MsgRevokeTrustedIssuer", nil)
}

func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterIdentity{},
		&MsgRevokeIdentity{},
		&MsgRotateIdentityKey{},
		&MsgUpdateStatus{},
		&MsgSetGuardians{},
		&MsgInitiateRecovery{},
		&MsgApproveRecovery{},
		&MsgRejectRecovery{},
		&MsgExecuteRecovery{},
		&MsgCancelRecovery{},
		&MsgUpdateParams{},
		&MsgRegisterTrustedIssuer{},
		&MsgRevokeTrustedIssuer{},
	)
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
