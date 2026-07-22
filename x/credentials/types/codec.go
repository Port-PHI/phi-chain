// SPDX-License-Identifier: Apache-2.0

package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers the module messages for amino signing.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgRegisterCredentialTemplate{}, "phi/credentials/MsgRegisterCredentialTemplate", nil)
	cdc.RegisterConcrete(&MsgUpdateCredentialTemplate{}, "phi/credentials/MsgUpdateCredentialTemplate", nil)
	cdc.RegisterConcrete(&MsgDeprecateCredentialTemplate{}, "phi/credentials/MsgDeprecateCredentialTemplate", nil)
	cdc.RegisterConcrete(&MsgAnchorCredential{}, "phi/credentials/MsgAnchorCredential", nil)
	cdc.RegisterConcrete(&MsgRevokeCredential{}, "phi/credentials/MsgRevokeCredential", nil)
	cdc.RegisterConcrete(&MsgCreateAgreement{}, "phi/credentials/MsgCreateAgreement", nil)
	cdc.RegisterConcrete(&MsgSignAgreement{}, "phi/credentials/MsgSignAgreement", nil)
	cdc.RegisterConcrete(&MsgCancelAgreement{}, "phi/credentials/MsgCancelAgreement", nil)
	cdc.RegisterConcrete(&MsgAnchorPersonal{}, "phi/credentials/MsgAnchorPersonal", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "phi/credentials/MsgUpdateParams", nil)
}

// RegisterInterfaces registers the sdk.Msg implementations in the interface registry.
func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterCredentialTemplate{},
		&MsgUpdateCredentialTemplate{},
		&MsgDeprecateCredentialTemplate{},
		&MsgAnchorCredential{},
		&MsgRevokeCredential{},
		&MsgCreateAgreement{},
		&MsgSignAgreement{},
		&MsgCancelAgreement{},
		&MsgAnchorPersonal{},
		&MsgUpdateParams{},
	)
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
