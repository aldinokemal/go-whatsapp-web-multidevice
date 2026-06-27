package call

import (
	"context"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp/voip/core"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp/voip/media"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp/voip/signaling"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp/voip/wanode"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

func (m *CallManager) HandleCallOffer(ctx context.Context, node *waBinary.Node, peerJid types.JID) {
	info := signaling.ExtractNodeInfo(node)
	if info == nil {
		return
	}
	callID := info.CallID
	creator := wanode.AttrString(info.InnerNode.Attrs, "call-creator")
	if creator == "" {
		creator = peerJid.String()
	}
	isVideo := hasChildTag(info.InnerNode, "video")

	callKey, err := signaling.DecryptCallKeyInNode(ctx, m.sock, info.InnerNode, peerJid)
	if err != nil {
		m.log.Error("offer decrypt call key", "err", err)
	}
	relays := signaling.ExtractRelayEndpoints(info.InnerNode)
	var structured *signaling.ParsedRelayAck
	if len(relays) == 0 {
		// The offer may carry relays in the structured <relay><te2> form (the
		// same encoding acks use) rather than as <relay ip=.. token=..>
		// attributes. ExtractRelayEndpoints only reads the attribute form, so
		// fall back to the structured parser before giving up.
		if parsed := signaling.ParseRelayFromAck(info.InnerNode); len(parsed.Relays) > 0 {
			relays = parsed.Relays
			structured = &parsed
			m.log.Info("offer relays parsed via structured (te2) format", "call_id", callID, "relays", len(relays))
		}
	}
	// Diagnostic: show the offer's child structure so we can see whether relays
	// are present (and in which form) or genuinely arrive later.
	m.log.Debug("offer inner node structure", "call_id", callID, "children", childTagSummary(info.InnerNode))

	mediaType := core.CallMediaTypeAudio
	if isVideo {
		mediaType = core.CallMediaTypeVideo
	}

	m.mu.Lock()
	call := NewIncomingCall(callID, peerJid.String(), creator, "", mediaType)
	if callKey != nil {
		call.EncryptionKey = callKey
	}
	if len(relays) > 0 {
		rd := &core.RelayData{Endpoints: relays}
		if structured != nil {
			// Carry the full structured data so SRTP/SSRC setup has participants.
			rd.ParticipantJids = structured.ParticipantJids
			rd.UUID = structured.UUID
			rd.SelfPid = structured.SelfPid
			rd.PeerPid = structured.PeerPid
			rd.HbhKey = structured.HbhKey
		}
		call.RelayData = rd
	}
	m.currentCall = call
	m.initialTransportSent = false

	selfJid := m.sock.OwnLID()
	sj := selfJid.String()
	if selfJid.IsEmpty() {
		sj = m.sock.OwnPN().String()
	}
	m.selfSsrc = media.GenerateSecureSsrc(callID, sj, 0)
	m.rtpSession = media.NewWhatsAppOpusSession(m.selfSsrc)
	m.peerSsrcs = []uint32{media.GenerateSecureSsrc(callID, peerJid.String(), 0)}
	m.initCodec()
	m.mu.Unlock()

	preaccept := signaling.BuildPreacceptStanza(peerJid, callID, wanode.MustJID(creator))
	if err := m.sock.SendNode(ctx, preaccept); err != nil {
		m.log.Error("send preaccept", "err", err)
	}

	if m.OnIncoming != nil {
		m.OnIncoming(call)
	}
	m.mu.Lock()
	m.emitState()
	m.mu.Unlock()
	m.log.Info("incoming call", "call_id", callID, "peer", peerJid.String(), "video", isVideo, "relays", len(relays))
}

func (m *CallManager) HandleCallAccept(ctx context.Context, node *waBinary.Node, peerJid types.JID) {
	m.mu.Lock()
	call := m.currentCall
	m.mu.Unlock()
	if call == nil {
		return
	}
	info := signaling.ExtractNodeInfo(node)
	if info == nil {
		return
	}

	if signaling.NeedsDecryption(info.Tag) {
		if peerKey, err := signaling.DecryptCallKeyInNode(ctx, m.sock, info.InnerNode, peerJid); err == nil && peerKey != nil {
			m.mu.Lock()
			if call.EncryptionKey != nil && !equalBytes(call.EncryptionKey, peerKey) {
				m.reinitSrtpLocked(peerKey, peerJid)
			}
			m.mu.Unlock()
		}
	}

	m.mu.Lock()
	_ = call.ApplyTransition(Transition{Type: TransitionRemoteAccepted})
	m.emitState()
	m.acceptedByJid = peerJid.String()
	if m.peerSsrcs == nil || !m.actualPeerSet {
		peerDeviceJid := ensureDeviceJid(peerJid.String())
		m.peerSsrcs = []uint32{media.GenerateSecureSsrc(call.CallID, peerDeviceJid, 0)}
	}
	m.relay.SetSubscriptionSsrc(firstSsrc(m.peerSsrcs))
	m.initSrtpKeysLocked()
	hasConn := m.relay.HasConnection()
	relayData := call.RelayData
	m.mu.Unlock()

	m.log.Info("remote accepted call", "call_id", call.CallID, "peer", peerJid.String(),
		"relay_connected", hasConn, "relay_endpoints", relayEndpointCount(relayData))

	m.relay.ResendSubscriptions()

	callID := call.CallID
	creator := wanode.MustJID(call.CallCreator)
	transport := waBinary.Node{
		Tag:   "call",
		Attrs: waBinary.Attrs{"to": peerJid, "id": signaling.GenerateCallStanzaID()},
		Content: []waBinary.Node{{
			Tag: "transport",
			Attrs: waBinary.Attrs{
				"call-id": callID, "call-creator": creator,
				"transport-message-type": "1", "p2p-cand-round": "1",
			},
			Content: []waBinary.Node{{Tag: "net", Attrs: waBinary.Attrs{"medium": "2", "protocol": "0"}}},
		}},
	}
	_ = m.sock.SendNode(ctx, transport)
	_ = m.sock.SendNode(ctx, signaling.BuildMuteV2Stanza(peerJid, callID, creator, 0))
	if acceptMsgID := wanode.AttrString(node.Attrs, "id"); acceptMsgID != "" {
		ourJid := m.sock.OwnLID()
		if ourJid.IsEmpty() {
			ourJid = m.sock.OwnPN()
		}
		_ = m.sock.SendNode(ctx, signaling.BuildAcceptReceiptStanza(peerJid, acceptMsgID, callID, creator, ourJid))
	}

	if hasConn {
		m.mu.Lock()
		if err := call.ApplyTransition(Transition{Type: TransitionMediaConnected}); err == nil {
			m.emitState()
			m.startSilenceKeepaliveLocked()
			m.log.Info("call ACTIVE (media path established)", "call_id", call.CallID, "audio", m.codec != nil)
		}
		m.mu.Unlock()
	} else if relayData != nil {
		m.connectRelays(relayData.Endpoints)
	}
}

func (m *CallManager) HandleCallTransport(ctx context.Context, node *waBinary.Node, peerJid types.JID) {
	m.mu.Lock()
	call := m.currentCall
	m.mu.Unlock()
	if call == nil {
		return
	}
	info := signaling.ExtractNodeInfo(node)
	if info == nil {
		return
	}
	relays := signaling.ExtractRelayEndpoints(info.InnerNode)
	m.log.Info("call transport received", "call_id", call.CallID,
		"relays", len(relays), "already_connected", m.relay.HasConnection())
	if len(relays) > 0 && !m.relay.HasConnection() {
		m.mu.Lock()
		if call.RelayData == nil {
			call.RelayData = &core.RelayData{}
		}
		call.RelayData.Endpoints = relays
		m.mu.Unlock()
		m.connectRelays(relays)
	}
}

func (m *CallManager) HandleCallAck(ctx context.Context, node *waBinary.Node) {
	if t := wanode.AttrString(node.Attrs, "type"); t != "offer" {
		return
	}
	if e := wanode.AttrString(node.Attrs, "error"); e != "" {
		m.log.Error("offer ack error", "error", e)
		return
	}
	parsed := signaling.ParseRelayFromAck(node)
	m.log.Info("offer ack received", "relays", len(parsed.Relays), "participants", len(parsed.ParticipantJids))
	if len(parsed.Relays) == 0 {
		return
	}

	m.mu.Lock()
	call := m.currentCall
	if call == nil {
		m.mu.Unlock()
		return
	}
	call.RelayData = &core.RelayData{
		Endpoints:       parsed.Relays,
		ParticipantJids: parsed.ParticipantJids,
		UUID:            parsed.UUID,
		SelfPid:         parsed.SelfPid,
		PeerPid:         parsed.PeerPid,
		HbhKey:          parsed.HbhKey,
	}

	ourBase := wanode.CleanJID(m.ownCredJid())
	if len(parsed.ParticipantJids) > 0 {
		ourDeviceJid := ensureDeviceJid(findOurDevice(parsed.ParticipantJids, ourBase, m.ownCredJid()))
		newSelf := media.GenerateSecureSsrc(call.CallID, ourDeviceJid, 0)
		if newSelf != m.selfSsrc {
			m.selfSsrc = newSelf
			m.rtpSession = media.NewWhatsAppOpusSession(newSelf)
		}
		if peer := firstPeerDevice(parsed.ParticipantJids, ourBase); peer != "" {
			m.peerSsrcs = []uint32{media.GenerateSecureSsrc(call.CallID, ensureDeviceJid(peer), 0)}
		}
		if call.EncryptionKey != nil {
			m.initSrtpKeysLocked()
		}
	}
	isInitiator := call.IsInitiator()
	peer := wanode.MustJID(call.PeerJid)
	callID := call.CallID
	creator := wanode.MustJID(call.CallCreator)
	sendPreaccept := isInitiator && !m.outgoingPreacceptSent
	if sendPreaccept {
		m.outgoingPreacceptSent = true
	}
	endpoints := parsed.Relays
	m.mu.Unlock()

	if sendPreaccept {
		_ = m.sock.SendNode(ctx, signaling.BuildPreacceptStanza(peer, callID, creator))
	}
	m.connectRelays(endpoints)
}

func (m *CallManager) HandleCallTerminate(node *waBinary.Node) {
	m.mu.Lock()
	call := m.currentCall
	if call == nil {
		m.mu.Unlock()
		return
	}
	info := signaling.ExtractNodeInfo(node)
	reason := core.EndCallReasonUserEnded
	if info != nil {
		if r := wanode.AttrString(info.InnerNode.Attrs, "reason"); r != "" {
			reason = core.EndCallReason(r)
		}
	}
	m.log.Info("call terminated by peer", "call_id", call.CallID, "reason", string(reason))
	_ = call.ApplyTransition(Transition{Type: TransitionTerminated, Reason: reason})
	ended := call
	m.emitState()
	m.mu.Unlock()

	if m.OnEnded != nil {
		m.OnEnded(ended)
	}
	m.cleanupMedia()
}
