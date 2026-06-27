package transport

import (
	"fmt"
	"log/slog"
	"regexp"
	"sync"
	"time"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp/voip/core"

	"github.com/pion/webrtc/v4"
)

const (
	relayConnectionTimeout = 20 * time.Second
	relayKeepaliveInterval = 1100 * time.Millisecond
)

type relayConnState int

const (
	relayStateConnecting relayConnState = iota
	relayStateOpen
	relayStateClosed
	relayStateFailed
)

type RelayConfig struct {
	IP           string
	Port         int
	Token        string
	AuthToken    string
	RawAuthToken []byte
	RawToken     []byte
	Key          string
	RelayID      int
	Name         string
	AuthTokenID  string
}

type relayConnection struct {
	state      relayConnState
	pc         *webrtc.PeerConnection
	channel    *webrtc.DataChannel
	id         string
	info       RelayConfig
	localUfrag string
	keepalive  *time.Ticker
	stopCh     chan struct{}
}

type SctpRelayManager struct {
	mu          sync.Mutex
	connections map[string]*relayConnection
	log         *slog.Logger

	audioSsrc        uint32
	subscriptionSsrc uint32

	onConnected func(ip string, port int)

	onReceive func(data []byte)
}

func NewSctpRelayManager(log *slog.Logger) *SctpRelayManager {
	if log == nil {
		log = slog.Default()
	}
	return &SctpRelayManager{
		connections: map[string]*relayConnection{},
		log:         log,
	}
}

func (m *SctpRelayManager) SetSsrc(ssrc uint32) { m.audioSsrc = ssrc }

func (m *SctpRelayManager) SetSubscriptionSsrc(ssrc uint32) { m.subscriptionSsrc = ssrc }

func (m *SctpRelayManager) SetOnConnected(fn func(ip string, port int)) { m.onConnected = fn }

func (m *SctpRelayManager) SetOnReceive(fn func(data []byte)) { m.onReceive = fn }

func (m *SctpRelayManager) ResendSubscriptions() {
	m.mu.Lock()
	conns := make([]*relayConnection, 0, len(m.connections))
	for _, c := range m.connections {
		conns = append(conns, c)
	}
	m.mu.Unlock()
	for _, c := range conns {
		if c.state == relayStateOpen && c.channel != nil {
			m.sendStunRegistration(c)
		}
	}
}

func connID(ip string, port int, authTokenID string) string {
	base := fmt.Sprintf("%s:%d", ip, port)
	if authTokenID != "" {
		return base + "#" + authTokenID
	}
	return base
}

func (m *SctpRelayManager) ConfigureRelays(relays []RelayConfig) {
	var wg sync.WaitGroup
	for _, r := range relays {
		port := r.Port
		if port == 0 {
			port = core.WARelayPort
		}
		r.Port = port
		id := connID(r.IP, port, r.AuthTokenID)
		m.mu.Lock()
		_, exists := m.connections[id]
		m.mu.Unlock()
		if exists {
			continue
		}
		wg.Add(1)
		go func(rc RelayConfig) {
			defer wg.Done()
			m.connectToRelay(rc)
		}(r)
	}
	wg.Wait()
}

func (m *SctpRelayManager) connectToRelay(info RelayConfig) {
	id := connID(info.IP, info.Port, info.AuthTokenID)
	m.log.Info("relay connecting", "id", id, "name", info.Name)

	conn := &relayConnection{
		state:  relayStateConnecting,
		id:     id,
		info:   info,
		stopCh: make(chan struct{}),
	}
	m.mu.Lock()
	m.connections[id] = conn
	m.mu.Unlock()

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		m.log.Error("relay peerconnection failed", "id", id, "err", err)
		m.failConnection(conn)
		return
	}
	conn.pc = pc

	pc.OnICEConnectionStateChange(func(s webrtc.ICEConnectionState) {
		m.log.Info("relay ice state", "id", id, "state", s.String())
		if s == webrtc.ICEConnectionStateFailed || s == webrtc.ICEConnectionStateDisconnected {
			m.failConnection(conn)
		}
	})

	ordered := false
	channel, err := pc.CreateDataChannel("wa-web-call", &webrtc.DataChannelInit{Ordered: &ordered})
	if err != nil {
		m.log.Error("relay datachannel failed", "id", id, "err", err)
		m.failConnection(conn)
		return
	}
	conn.channel = channel

	channel.OnOpen(func() {
		m.log.Info("relay datachannel open", "id", id)
		conn.state = relayStateOpen
		m.sendStunRegistration(conn)
		m.startKeepalive(conn)
		if m.onConnected != nil {
			m.onConnected(info.IP, info.Port)
		}
	})
	channel.OnClose(func() { m.closeConnection(id) })
	channel.OnMessage(func(msg webrtc.DataChannelMessage) {
		if m.onReceive != nil {
			m.onReceive(msg.Data)
		}
	})

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		m.failConnection(conn)
		return
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		m.failConnection(conn)
		return
	}

	conn.localUfrag = extractFirst(reUfrag, offer.SDP)
	munged := m.modifySdpForRelay(offer.SDP, info)

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: munged}); err != nil {
		m.log.Error("relay set remote description failed", "id", id, "err", err)
		m.failConnection(conn)
		return
	}

	go func() {
		select {
		case <-time.After(relayConnectionTimeout):
			if conn.state == relayStateConnecting {
				m.log.Debug("relay connection timeout", "id", id)
				m.failConnection(conn)
			}
		case <-conn.stopCh:
		}
	}()
}

var (
	reSetup       = regexp.MustCompile(`a=setup:actpass`)
	reUfragLine   = regexp.MustCompile(`a=ice-ufrag:[^\r\n]+`)
	rePwdLine     = regexp.MustCompile(`a=ice-pwd:[^\r\n]+`)
	reFingerprint = regexp.MustCompile(`a=fingerprint:[^\r\n]+`)
	reMaxMsg      = regexp.MustCompile(`a=max-message-size:[^\r\n]+`)
	reIceOptions  = regexp.MustCompile(`a=ice-options:[^\r\n]+\r?\n`)
	reCandidate   = regexp.MustCompile(`a=candidate:[^\r\n]+\r?\n`)
	reEndCand     = regexp.MustCompile(`a=end-of-candidates\r?\n?`)
	reUfrag       = regexp.MustCompile(`a=ice-ufrag:([^\r\n]+)`)
)

func (m *SctpRelayManager) modifySdpForRelay(sdp string, info RelayConfig) string {
	out := reSetup.ReplaceAllString(sdp, "a=setup:passive")

	iceUfrag := info.AuthToken
	if iceUfrag == "" {
		iceUfrag = info.Token
	}
	out = reUfragLine.ReplaceAllString(out, "a=ice-ufrag:"+iceUfrag)
	out = rePwdLine.ReplaceAllString(out, "a=ice-pwd:"+info.Key)
	out = reFingerprint.ReplaceAllString(out, "a=fingerprint:"+core.WADTLSFingerprint)
	out = reMaxMsg.ReplaceAllString(out, "a=max-message-size:1500")
	out = reIceOptions.ReplaceAllString(out, "")

	out = reCandidate.ReplaceAllString(out, "")
	out = reEndCand.ReplaceAllString(out, "")
	candidate := fmt.Sprintf("a=candidate:2 1 udp 2122262783 %s %d typ host generation 0 network-cost 5", info.IP, info.Port)
	out += candidate + "\r\n" + "a=end-of-candidates" + "\r\n"
	return out
}

func extractFirst(re *regexp.Regexp, s string) string {
	if mm := re.FindStringSubmatch(s); len(mm) > 1 {
		return mm[1]
	}
	return ""
}

func (m *SctpRelayManager) sendStunRegistration(conn *relayConnection) {
	info := conn.info
	remoteUfrag := info.AuthToken
	if remoteUfrag == "" {
		remoteUfrag = info.Token
	}
	if remoteUfrag == "" {
		return
	}
	localUfrag := conn.localUfrag
	hmacKey := []byte(info.Key)

	send := func() {
		if conn.state != relayStateOpen || conn.channel == nil {
			return
		}
		ssrc := m.subscriptionSsrc
		if ssrc == 0 {
			ssrc = m.audioSsrc
		}
		if ssrc == 0 {
			return
		}
		subs := BuildSenderSubscriptions(ssrc)

		if localUfrag != "" {
			username := []byte(remoteUfrag + ":" + localUfrag)
			m.sendRaw(conn, BuildBindingRequestWithSubs(username, hmacKey, subs, true, true))
		}
		if info.Token != "" && info.Token != remoteUfrag && localUfrag != "" {
			username := []byte(info.Token + ":" + localUfrag)
			m.sendRaw(conn, BuildBindingRequestWithSubs(username, hmacKey, subs, true, true))
		}
		m.sendRaw(conn, BuildBindingRequestWithSubs(nil, nil, subs, false, false))

		if len(info.RawToken) > 0 {
			var peerSsrcs []uint32
			if m.subscriptionSsrc != 0 {
				peerSsrcs = []uint32{m.subscriptionSsrc}
			}
			ssrcList := BuildSSRCSubscriptionList([]uint32{m.audioSsrc}, peerSsrcs, 0, 0)
			m.sendRaw(conn, BuildAllocateForRelay(info.RawToken, ssrcList, hmacKey, info.IP, info.Port))
		}
	}

	send()
	for _, d := range []time.Duration{50, 150, 500, 3000} {
		delay := d * time.Millisecond
		go func() {
			select {
			case <-time.After(delay):
				m.mu.Lock()
				open := conn.state == relayStateOpen
				m.mu.Unlock()
				if open {
					send()
				}
			case <-conn.stopCh:
			}
		}()
	}
}

func (m *SctpRelayManager) startKeepalive(conn *relayConnection) {
	m.sendRaw(conn, BuildWhatsAppPing())
	ticker := time.NewTicker(relayKeepaliveInterval)
	conn.keepalive = ticker
	go func() {
		for {
			select {
			case <-ticker.C:
				if conn.state != relayStateOpen || conn.channel == nil {
					return
				}
				m.sendRaw(conn, BuildWhatsAppPing())
			case <-conn.stopCh:
				ticker.Stop()
				return
			}
		}
	}()
}

func (m *SctpRelayManager) sendRaw(conn *relayConnection, data []byte) {
	if conn.channel == nil || conn.state != relayStateOpen {
		return
	}
	if err := conn.channel.Send(data); err != nil {
		m.log.Debug("relay send error", "id", conn.id, "err", err)
	}
}

func (m *SctpRelayManager) Broadcast(data []byte) {
	m.mu.Lock()
	conns := make([]*relayConnection, 0, len(m.connections))
	for _, c := range m.connections {
		conns = append(conns, c)
	}
	m.mu.Unlock()
	for _, c := range conns {
		m.sendRaw(c, data)
	}
}

func (m *SctpRelayManager) HasConnection() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.connections {
		if c.state == relayStateOpen {
			return true
		}
	}
	return false
}

func (m *SctpRelayManager) ConnectedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, c := range m.connections {
		if c.state == relayStateOpen {
			n++
		}
	}
	return n
}

func (m *SctpRelayManager) failConnection(conn *relayConnection) {
	m.mu.Lock()
	if conn.state == relayStateFailed {
		m.mu.Unlock()
		return
	}
	conn.state = relayStateFailed
	delete(m.connections, conn.id)
	m.mu.Unlock()
	m.teardown(conn)
}

func (m *SctpRelayManager) closeConnection(id string) {
	m.mu.Lock()
	conn := m.connections[id]
	if conn == nil {
		m.mu.Unlock()
		return
	}
	conn.state = relayStateClosed
	delete(m.connections, id)
	m.mu.Unlock()
	m.teardown(conn)
}

func (m *SctpRelayManager) teardown(conn *relayConnection) {
	select {
	case <-conn.stopCh:
	default:
		close(conn.stopCh)
	}
	if conn.keepalive != nil {
		conn.keepalive.Stop()
	}
	if conn.channel != nil {
		_ = conn.channel.Close()
	}
	if conn.pc != nil {
		_ = conn.pc.Close()
	}
}

func (m *SctpRelayManager) Cleanup() {
	m.mu.Lock()
	conns := make([]*relayConnection, 0, len(m.connections))
	for _, c := range m.connections {
		conns = append(conns, c)
	}
	m.connections = map[string]*relayConnection{}
	m.audioSsrc = 0
	m.subscriptionSsrc = 0
	m.mu.Unlock()
	for _, c := range conns {
		m.teardown(c)
	}
}
