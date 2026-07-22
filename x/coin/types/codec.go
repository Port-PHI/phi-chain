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
	cdc.RegisterConcrete(&MsgTransfer{}, "phi/coin/MsgTransfer", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "phi/coin/MsgUpdateParams", nil)
	cdc.RegisterConcrete(&MsgWithdrawRevenue{}, "phi/coin/MsgWithdrawRevenue", nil)
}

// RegisterInterfaces registers the sdk.Msg implementations.
func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgTransfer{},
		&MsgUpdateParams{},
		&MsgWithdrawRevenue{},
	)
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
