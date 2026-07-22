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
	cdc.RegisterConcrete(&MsgCreateElection{}, "phi/voting/MsgCreateElection", nil)
	cdc.RegisterConcrete(&MsgCastVote{}, "phi/voting/MsgCastVote", nil)
	cdc.RegisterConcrete(&MsgCloseElection{}, "phi/voting/MsgCloseElection", nil)
	cdc.RegisterConcrete(&MsgCancelElection{}, "phi/voting/MsgCancelElection", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "phi/voting/MsgUpdateParams", nil)
}

// RegisterInterfaces registers the sdk.Msg implementations in the interface registry.
func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateElection{},
		&MsgCastVote{},
		&MsgCloseElection{},
		&MsgCancelElection{},
		&MsgUpdateParams{},
	)
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
