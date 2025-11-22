package whatsapp

import (
	"fmt"
	"strings"

	waLog "go.mau.fi/whatsmeow/util/log"
)

// filteredLogger wraps the default whatsmeow logger to downgrade noisy websocket EOF errors
// that are expected during reconnect cycles. Without this wrapper, the library logs those EOFs
// as errors even though the client automatically reconnects and continues working.
type filteredLogger struct {
	base waLog.Logger
}

const websocketEOFErrorMsg = "Error reading from websocket: failed to get reader: failed to read frame header: EOF"

func isWebsocketEOFError(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, strings.ToLower(websocketEOFErrorMsg)) ||
		(strings.Contains(lower, "error reading from websocket") && strings.Contains(lower, "failed to read frame header: eof"))
}

func newFilteredLogger(base waLog.Logger) waLog.Logger {
	return &filteredLogger{base: base}
}

func (l *filteredLogger) Errorf(msg string, args ...interface{}) {
	formatted := fmt.Sprintf(msg, args...)
	if isWebsocketEOFError(formatted) {
		l.base.Debugf("WebSocket closed after idle; auto-reconnecting within ~1s without interrupting message handling. Investigate only if reconnection keeps failing: %s", formatted)
		return
	}

	l.base.Errorf(msg, args...)
}

func (l *filteredLogger) Warnf(msg string, args ...interface{}) {
	l.base.Warnf(msg, args...)
}

func (l *filteredLogger) Infof(msg string, args ...interface{}) {
	l.base.Infof(msg, args...)
}

func (l *filteredLogger) Debugf(msg string, args ...interface{}) {
	l.base.Debugf(msg, args...)
}

func (l *filteredLogger) Sub(module string) waLog.Logger {
	return newFilteredLogger(l.base.Sub(module))
}
