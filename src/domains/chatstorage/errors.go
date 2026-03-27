package chatstorage

import "errors"

// ErrMissingDeviceContext is returned when CreateIncomingCallRecord cannot resolve
// the WhatsApp client from context (e.g. context wiring regression).
var ErrMissingDeviceContext = errors.New("missing WhatsApp client/device context for incoming call persistence")

// ErrCallOfferMissingPeerJID is returned when a CallOffer has no group or peer From JID to map to a chat row.
var ErrCallOfferMissingPeerJID = errors.New("unable to resolve peer JID for CallOffer")
