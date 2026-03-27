package chatstorage

import "errors"

// ErrMissingDeviceContext is returned when CreateIncomingCallRecord cannot resolve
// the WhatsApp client from context (e.g. context wiring regression).
var ErrMissingDeviceContext = errors.New("missing WhatsApp client/device context for incoming call persistence")
