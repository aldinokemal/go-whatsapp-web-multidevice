package call

import (
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp/voip/core"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp/voip/transport"
)

type RelayTransport interface {
	SetSsrc(ssrc uint32)
	SetSubscriptionSsrc(ssrc uint32)
	SetOnConnected(fn func(ip string, port int))
	SetOnReceive(fn func(data []byte))
	ResendSubscriptions()
	ConfigureRelays(relays []transport.RelayConfig)
	Broadcast(data []byte)
	HasConnection() bool
	ConnectedCount() int
	Cleanup()
}

var _ RelayTransport = (*transport.SctpRelayManager)(nil)

func (m *CallManager) onRelayConnected() {
	m.mu.Lock()
	call := m.currentCall
	if call != nil && call.StateData.State == core.CallStateConnecting {
		if err := call.ApplyTransition(Transition{Type: TransitionMediaConnected}); err == nil {
			m.emitState()
			m.startSilenceKeepaliveLocked()
			m.log.Info("relay connected → active", "call_id", call.CallID)
		}
	}
	m.mu.Unlock()
}

func buildRelayConfigs(endpoints []core.RelayEndpoint) []transport.RelayConfig {
	seen := map[string]bool{}
	var relays []transport.RelayConfig
	for _, ep := range endpoints {
		if ep.Protocol != 0 {
			continue
		}
		if ep.Key == "" || ep.RawToken == nil {
			continue
		}
		key := ep.IP
		if seen[key] {
			continue
		}
		seen[key] = true
		name := ep.RelayName
		if name == "" {
			name = ep.IP
		}
		relays = append(relays, transport.RelayConfig{
			IP: ep.IP, Port: 3478, Token: ep.Token, AuthToken: ep.AuthToken,
			RawAuthToken: ep.RawAuthToken, RawToken: ep.RawToken, Key: ep.Key,
			RelayID: ep.RelayID, Name: name, AuthTokenID: ep.AuthTokenID,
		})
	}
	return relays
}

func (m *CallManager) connectRelays(endpoints []core.RelayEndpoint) {
	relays := buildRelayConfigs(endpoints)
	if len(relays) == 0 {
		m.log.Error("no usable relay configs")
		return
	}
	m.mu.Lock()
	m.relay.SetSsrc(m.selfSsrc)
	m.relay.SetSubscriptionSsrc(firstSsrc(m.peerSsrcs))
	m.mu.Unlock()
	m.relay.ConfigureRelays(relays)
	m.log.Info("relay configured", "connected", m.relay.ConnectedCount())
}

func (m *CallManager) cleanupMedia() {
	m.mu.Lock()
	codec := m.codec
	m.codec = nil
	if m.keepaliveStop != nil {
		close(m.keepaliveStop)
		m.keepaliveStop = nil
	}
	m.rtpSession = nil
	m.srtpSession = nil
	m.firstPacketSent = false
	m.initialTransportSent = false
	m.outgoingPreacceptSent = false
	m.actualPeerSet = false
	m.encodeBuf = nil
	m.encodeBufPos = 0
	m.mu.Unlock()

	m.relay.Cleanup()
	if codec != nil {
		codec.Close()
	}
}
