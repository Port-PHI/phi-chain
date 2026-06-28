// SPDX-License-Identifier: Apache-2.0

package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers the messages for amino signing.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgRegisterIdentity{}, "phi/identity/MsgRegisterIdentity", nil)
	cdc.RegisterConcrete(&MsgRevokeIdentity{}, "phi/identity/MsgRevokeIdentity", nil)
	cdc.RegisterConcrete(&MsgRotateIdentityKey{}, "phi/identity/MsgRotateIdentityKey", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "phi/identity/MsgUpdateParams", nil)
	cdc.RegisterConcrete(&MsgRegisterTrustedIssuer{}, "phi/identity/MsgRegisterTrustedIssuer", nil)
	cdc.RegisterConcrete(&MsgRevokeTrustedIssuer{}, "phi/identity/MsgRevokeTrustedIssuer", nil)
}

// RegisterInterfaces registers the sdk.Msg implementations in the interface registry.
func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterIdentity{},
		&MsgRevokeIdentity{},
		&MsgRotateIdentityKey{},
		&MsgUpdateParams{},
		&MsgRegisterTrustedIssuer{},
		&MsgRevokeTrustedIssuer{},
	)
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
