package core

import (
	"context"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

type VoipSocket interface {
	OwnPN() types.JID

	OwnLID() types.JID

	AccountDeviceIdentityNode() (waBinary.Node, bool)

	SendNode(ctx context.Context, node waBinary.Node) error

	Query(ctx context.Context, node waBinary.Node) (*waBinary.Node, error)

	GetUSyncDevices(ctx context.Context, jids []types.JID) ([]types.JID, error)

	AssertSessions(ctx context.Context, jids []types.JID, force bool) error

	CreateParticipantNodes(ctx context.Context, devices []types.JID, callKey []byte, encAttrs waBinary.Attrs) ([]waBinary.Node, bool, error)

	DecryptCallKey(ctx context.Context, from types.JID, encChild *waBinary.Node) ([]byte, error)

	GetTCToken(ctx context.Context, jid types.JID) ([]byte, error)

	ResolveLIDForPN(ctx context.Context, pn types.JID) types.JID
}
