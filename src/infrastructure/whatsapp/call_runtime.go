package whatsapp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	voipbridge "github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp/voip/bridge"
	voipcall "github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp/voip/call"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp/voip/core"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp/voip/signaling"
	voipsocket "github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp/voip/socket"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/websocket"
	"github.com/sirupsen/logrus"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type activeVoiceCall struct {
	deviceID  string
	manager   *voipcall.CallManager
	bridge    *voipbridge.Bridge
	recording *callRecorder
	info      domainCall.CallInfo
}

type VoiceCallRuntime struct {
	mu    sync.Mutex
	calls map[string]map[string]*activeVoiceCall
}

var (
	defaultVoiceCallRuntime     *VoiceCallRuntime
	defaultVoiceCallRuntimeOnce sync.Once
)

func GetCallRuntime() *VoiceCallRuntime {
	defaultVoiceCallRuntimeOnce.Do(func() {
		defaultVoiceCallRuntime = NewVoiceCallRuntime()
	})
	return defaultVoiceCallRuntime
}

func NewVoiceCallRuntime() *VoiceCallRuntime {
	return &VoiceCallRuntime{calls: make(map[string]map[string]*activeVoiceCall)}
}

func (r *VoiceCallRuntime) StartCall(ctx context.Context, device *DeviceInstance, phone string, record bool) (domainCall.CallInfo, error) {
	if device == nil || device.GetClient() == nil {
		return domainCall.CallInfo{}, fmt.Errorf("device is not logged in")
	}
	client := device.GetClient()
	if client.Store == nil || client.Store.ID == nil {
		return domainCall.CallInfo{}, fmt.Errorf("device is not paired")
	}

	callID := signaling.GenerateCallID()
	peer := types.NewJID(normalizeCallPhone(phone), types.DefaultUserServer)
	var recorder *callRecorder
	var err error
	if record {
		recorder, err = newCallRecorder(callRecordingPath(device.ID(), callID))
		if err != nil {
			return domainCall.CallInfo{}, err
		}
	}
	manager := r.newManager(ctx, device, callID)
	if err := manager.StartCall(ctx, callID, peer, false); err != nil {
		if recorder != nil {
			_ = recorder.Close()
		}
		r.remove(device.ID(), callID)
		return domainCall.CallInfo{}, err
	}

	info := domainCall.CallInfo{
		DeviceID:  device.ID(),
		CallID:    callID,
		PeerJID:   peer.String(),
		Direction: domainCall.DirectionOutbound,
		Status:    domainCall.StatusRinging,
		MediaType: domainCall.MediaTypeAudio,
		StartedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if recorder != nil {
		info.Recording = true
		info.RecordingPath = recorder.Path()
		info.RecordingURL = callRecordingURL(recorder.Path())
		info.RecordingFormat = callRecordingFormat
		info.Metadata = callRecordingMetadataJSON(info)
	}
	r.upsertRuntimeCall(device.ID(), callID, &activeVoiceCall{deviceID: device.ID(), manager: manager, recording: recorder, info: info})
	r.persist(device, info)
	r.emit("CALL_STARTED", "Call started", info)
	return info, nil
}

func (r *VoiceCallRuntime) AcceptCall(ctx context.Context, deviceID, callID string, record bool) (domainCall.CallInfo, error) {
	call, ok := r.getRuntimeCall(deviceID, callID)
	if !ok {
		return domainCall.CallInfo{}, fmt.Errorf("call %s not found", callID)
	}
	if record && call.recording == nil {
		recorder, err := newCallRecorder(callRecordingPath(deviceID, callID))
		if err != nil {
			return domainCall.CallInfo{}, err
		}
		call.recording = recorder
		call.info.Recording = true
		call.info.RecordingPath = recorder.Path()
		call.info.RecordingURL = callRecordingURL(recorder.Path())
		call.info.RecordingFormat = callRecordingFormat
		call.info.Metadata = callRecordingMetadataJSON(call.info)
	}
	if err := call.manager.AcceptCall(ctx, callID); err != nil {
		if record && call.recording != nil && call.info.RecordingPath != "" {
			_ = call.recording.Close()
			call.recording = nil
		}
		return domainCall.CallInfo{}, err
	}
	return r.GetCallInfo(deviceID, callID)
}

func (r *VoiceCallRuntime) RejectCall(ctx context.Context, deviceID, callID string) (domainCall.CallInfo, error) {
	call, ok := r.getRuntimeCall(deviceID, callID)
	if !ok {
		return domainCall.CallInfo{}, fmt.Errorf("call %s not found", callID)
	}
	info := markCallEnded(call.info, "declined")
	if err := call.manager.RejectCall(ctx, callID, core.EndCallReasonDeclined); err != nil {
		return domainCall.CallInfo{}, err
	}
	r.closeRecordingOnly(deviceID, callID)
	return info, nil
}

func (r *VoiceCallRuntime) EndCall(ctx context.Context, deviceID, callID string) (domainCall.CallInfo, error) {
	call, ok := r.getRuntimeCall(deviceID, callID)
	if !ok {
		return domainCall.CallInfo{}, fmt.Errorf("call %s not found", callID)
	}
	info := markCallEnded(call.info, "user_ended")
	if err := call.manager.EndCall(ctx, core.EndCallReasonUserEnded); err != nil {
		return domainCall.CallInfo{}, err
	}
	r.closeRecordingOnly(deviceID, callID)
	return info, nil
}

func (r *VoiceCallRuntime) ExchangeWebRTC(ctx context.Context, deviceID, callID, sdpOffer string) (string, error) {
	call, ok := r.getRuntimeCall(deviceID, callID)
	if !ok {
		return "", fmt.Errorf("call %s not found", callID)
	}
	br, answer, err := voipbridge.NewBridge(sdpOffer, slog.Default())
	if err != nil {
		return "", err
	}
	br.OnBrowserPCM = func(pcm []float32) {
		if call.recording != nil {
			if err := call.recording.WriteLocal(pcm); err != nil {
				logrus.WithError(err).Debug("failed to write browser audio to call recording")
			}
		}
		call.manager.FeedCapturedPCM(pcm)
	}
	br.OnTerminalICE = func() {
		_, _ = r.EndCall(context.Background(), deviceID, callID)
	}

	r.mu.Lock()
	if current, ok := r.calls[deviceID][callID]; ok {
		if current.bridge != nil {
			current.bridge.Close()
		}
		current.bridge = br
	}
	r.mu.Unlock()

	return answer, nil
}

func (r *VoiceCallRuntime) GetCall(ctx context.Context, deviceID, callID string) (domainCall.CallInfo, bool) {
	_ = ctx
	info, err := r.GetCallInfo(deviceID, callID)
	if err != nil {
		return domainCall.CallInfo{}, false
	}
	return info, true
}

func (r *VoiceCallRuntime) ListCalls(ctx context.Context, deviceID string) []domainCall.CallInfo {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()

	deviceCalls := r.calls[deviceID]
	result := make([]domainCall.CallInfo, 0, len(deviceCalls))
	for _, call := range deviceCalls {
		result = append(result, call.info)
	}
	return result
}

func (r *VoiceCallRuntime) HandleIncomingOffer(ctx context.Context, device *DeviceInstance, evt *events.CallOffer) {
	if device == nil || evt == nil || evt.Data == nil {
		return
	}
	node := wrapCallNode(evt.From, evt.Data)
	callID := callIDFromNode(node)
	if callID == "" {
		return
	}
	manager := r.newManager(ctx, device, callID)
	manager.HandleCallOffer(ctx, node, evt.From)
}

func (r *VoiceCallRuntime) HandleAccept(ctx context.Context, device *DeviceInstance, evt *events.CallAccept) {
	if device == nil || evt == nil || evt.Data == nil {
		return
	}
	if call, ok := r.callForNode(device.ID(), evt.From, evt.Data); ok {
		call.manager.HandleCallAccept(ctx, wrapCallNode(evt.From, evt.Data), evt.From)
	}
}

func (r *VoiceCallRuntime) HandleTransport(ctx context.Context, device *DeviceInstance, evt *events.CallTransport) {
	if device == nil || evt == nil || evt.Data == nil {
		return
	}
	if call, ok := r.callForNode(device.ID(), evt.From, evt.Data); ok {
		call.manager.HandleCallTransport(ctx, wrapCallNode(evt.From, evt.Data), evt.From)
	}
}

func (r *VoiceCallRuntime) HandleTerminate(device *DeviceInstance, evt *events.CallTerminate) {
	if device == nil || evt == nil || evt.Data == nil {
		return
	}
	if call, ok := r.callForNode(device.ID(), evt.From, evt.Data); ok {
		call.manager.HandleCallTerminate(wrapCallNode(evt.From, evt.Data))
	}
}

func (r *VoiceCallRuntime) HandleReject(device *DeviceInstance, evt *events.CallReject) {
	if device == nil || evt == nil || evt.Data == nil {
		return
	}
	if call, ok := r.callForNode(device.ID(), evt.From, evt.Data); ok {
		call.manager.HandleCallTerminate(wrapCallNode(evt.From, evt.Data))
	}
}

func (r *VoiceCallRuntime) GetCallInfo(deviceID, callID string) (domainCall.CallInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if deviceCalls, ok := r.calls[deviceID]; ok {
		if call, ok := deviceCalls[callID]; ok {
			return call.info, nil
		}
	}
	return domainCall.CallInfo{}, fmt.Errorf("call %s not found", callID)
}

func (r *VoiceCallRuntime) newManager(ctx context.Context, device *DeviceInstance, callID string) *voipcall.CallManager {
	client := device.GetClient()
	manager := voipcall.NewCallManager(voipsocket.NewSocket(client), slog.Default())
	manager.OnIncoming = func(info *voipcall.CallInfo) {
		callInfo := domainInfoFromVoIP(device.ID(), info)
		r.upsertRuntimeCall(device.ID(), info.CallID, &activeVoiceCall{deviceID: device.ID(), manager: manager, info: callInfo})
		r.persist(device, callInfo)
		r.emit("CALL_INCOMING", "Incoming call", callInfo)
	}
	manager.OnStateChange = func(info *voipcall.CallInfo) {
		callInfo := domainInfoFromVoIP(device.ID(), info)
		callInfo = r.updateInfo(device.ID(), info.CallID, callInfo)
		r.persist(device, callInfo)
		r.emit("CALL_STATE", "Call state changed", callInfo)
		if info.IsEnded() {
			r.closeBridgeAndRemove(device.ID(), info.CallID)
		}
	}
	manager.OnEnded = func(info *voipcall.CallInfo) {
		callInfo := domainInfoFromVoIP(device.ID(), info)
		callInfo = r.updateInfo(device.ID(), info.CallID, callInfo)
		r.persist(device, callInfo)
		r.emit("CALL_ENDED", "Call ended", callInfo)
		r.closeBridgeAndRemove(device.ID(), info.CallID)
	}
	manager.OnPeerAudio = func(pcm []float32) {
		call, ok := r.getRuntimeCall(device.ID(), callID)
		if !ok {
			return
		}
		if call.recording != nil {
			if err := call.recording.WriteRemote(pcm); err != nil {
				logrus.WithError(err).Debug("failed to write remote audio to call recording")
			}
		}
		if call.bridge == nil {
			return
		}
		if err := call.bridge.WritePCM(pcm); err != nil {
			logrus.WithError(err).Debug("failed to write call audio to browser")
		}
	}

	_ = ctx
	return manager
}

func (r *VoiceCallRuntime) upsertRuntimeCall(deviceID, callID string, call *activeVoiceCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.calls[deviceID]; !ok {
		r.calls[deviceID] = make(map[string]*activeVoiceCall)
	}
	r.calls[deviceID][callID] = call
}

func (r *VoiceCallRuntime) updateInfo(deviceID, callID string, info domainCall.CallInfo) domainCall.CallInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.calls[deviceID]; !ok {
		r.calls[deviceID] = make(map[string]*activeVoiceCall)
	}
	if call, ok := r.calls[deviceID][callID]; ok {
		mergeCallRecordingInfo(&info, call.info)
		call.info = info
		return info
	}
	r.calls[deviceID][callID] = &activeVoiceCall{deviceID: deviceID, info: info}
	return info
}

func (r *VoiceCallRuntime) getRuntimeCall(deviceID, callID string) (*activeVoiceCall, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	deviceCalls, ok := r.calls[deviceID]
	if !ok {
		return nil, false
	}
	call, ok := deviceCalls[callID]
	return call, ok
}

func (r *VoiceCallRuntime) remove(deviceID, callID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if deviceCalls, ok := r.calls[deviceID]; ok {
		delete(deviceCalls, callID)
		if len(deviceCalls) == 0 {
			delete(r.calls, deviceID)
		}
	}
}

func (r *VoiceCallRuntime) closeBridgeAndRemove(deviceID, callID string) {
	r.mu.Lock()
	var br *voipbridge.Bridge
	var recorder *callRecorder
	if deviceCalls, ok := r.calls[deviceID]; ok {
		if call, ok := deviceCalls[callID]; ok {
			br = call.bridge
			recorder = call.recording
		}
		delete(deviceCalls, callID)
		if len(deviceCalls) == 0 {
			delete(r.calls, deviceID)
		}
	}
	r.mu.Unlock()
	if br != nil {
		br.Close()
	}
	if recorder != nil {
		_ = recorder.Close()
	}
}

func (r *VoiceCallRuntime) closeRecordingOnly(deviceID, callID string) {
	r.mu.Lock()
	var recorder *callRecorder
	if deviceCalls, ok := r.calls[deviceID]; ok {
		if call, ok := deviceCalls[callID]; ok {
			recorder = call.recording
			call.recording = nil
		}
	}
	r.mu.Unlock()
	if recorder != nil {
		_ = recorder.Close()
	}
}

func (r *VoiceCallRuntime) callForNode(deviceID string, from types.JID, data *waBinary.Node) (*activeVoiceCall, bool) {
	callID := callIDFromNode(wrapCallNode(from, data))
	if callID == "" {
		return nil, false
	}
	return r.getRuntimeCall(deviceID, callID)
}

func markCallEnded(info domainCall.CallInfo, reason string) domainCall.CallInfo {
	now := time.Now()
	info.Status = domainCall.StatusEnded
	info.EndReason = reason
	info.EndedAt = now
	info.UpdatedAt = now
	return info
}

func (r *VoiceCallRuntime) persist(device *DeviceInstance, info domainCall.CallInfo) {
	if device == nil || device.GetChatStorage() == nil {
		return
	}
	record := &domainChatStorage.CallRecord{
		DeviceID:  info.DeviceID,
		CallID:    info.CallID,
		PeerJID:   info.PeerJID,
		Direction: info.Direction,
		Status:    info.Status,
		MediaType: info.MediaType,
		StartedAt: info.StartedAt,
		UpdatedAt: info.UpdatedAt,
		EndReason: info.EndReason,
	}
	if !info.EndedAt.IsZero() {
		record.EndedAt = &info.EndedAt
	}
	if info.Recording {
		info.Metadata = callRecordingMetadataJSON(info)
	}
	record.Metadata = info.Metadata
	if err := device.GetChatStorage().StoreCallRecord(record); err != nil {
		logrus.WithError(err).Warn("failed to persist call record")
	}
}

func (r *VoiceCallRuntime) emit(code, message string, info domainCall.CallInfo) {
	msg := websocket.BroadcastMessage{
		Code:    code,
		Message: message,
		Result:  info,
	}
	select {
	case websocket.Broadcast <- msg:
	default:
		logrus.Debug("skipping call websocket broadcast: hub is not ready")
	}

	if event := callWebhookEventName(code, info); event != "" {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := forwardPayloadToConfiguredWebhooks(ctx, callWebhookPayload(event, info), event); err != nil {
				logrus.WithError(err).Warn("failed to forward call webhook")
			}
		}()
	}
}

func callWebhookEventName(code string, info domainCall.CallInfo) string {
	switch code {
	case "CALL_STARTED":
		return "call.started"
	case "CALL_INCOMING":
		return "call.incoming"
	case "CALL_ENDED":
		return "call.ended"
	case "CALL_STATE":
		switch info.Status {
		case domainCall.StatusConnecting:
			return "call.accept"
		case domainCall.StatusActive:
			return "call.active"
		case domainCall.StatusEnded:
			return "call.ended"
		default:
			return "call.state"
		}
	default:
		return ""
	}
}

func callWebhookPayload(event string, info domainCall.CallInfo) map[string]any {
	payload := map[string]any{
		"call_id":    info.CallID,
		"peer_jid":   info.PeerJID,
		"direction":  info.Direction,
		"status":     info.Status,
		"media_type": info.MediaType,
		"recording":  info.Recording,
	}
	if info.EndReason != "" {
		payload["end_reason"] = info.EndReason
	}
	if info.RecordingURL != "" {
		payload["recording_url"] = info.RecordingURL
	}
	if info.RecordingFormat != "" {
		payload["recording_format"] = info.RecordingFormat
	}

	body := map[string]any{
		"event":     event,
		"timestamp": time.Now().Format(time.RFC3339),
		"payload":   payload,
	}
	if info.DeviceID != "" {
		body["device_id"] = info.DeviceID
	}
	return body
}

func domainInfoFromVoIP(deviceID string, info *voipcall.CallInfo) domainCall.CallInfo {
	if info == nil {
		return domainCall.CallInfo{}
	}
	now := time.Now()
	direction := domainCall.DirectionOutbound
	if info.Direction == core.CallDirectionIncoming {
		direction = domainCall.DirectionInbound
	}
	callInfo := domainCall.CallInfo{
		DeviceID:  deviceID,
		CallID:    info.CallID,
		PeerJID:   info.PeerJid,
		Direction: direction,
		Status:    mapVoIPStatus(info.StateData.State),
		MediaType: string(info.MediaType),
		StartedAt: info.CreatedAt,
		UpdatedAt: now,
		EndReason: string(info.StateData.EndReason),
	}
	if info.StateData.EndedAt != nil {
		callInfo.EndedAt = *info.StateData.EndedAt
	}
	if callInfo.MediaType == "" {
		callInfo.MediaType = domainCall.MediaTypeAudio
	}
	return callInfo
}

func mergeCallRecordingInfo(dst *domainCall.CallInfo, src domainCall.CallInfo) {
	if dst == nil {
		return
	}
	if src.Recording {
		dst.Recording = true
	}
	if dst.RecordingPath == "" {
		dst.RecordingPath = src.RecordingPath
	}
	if dst.RecordingURL == "" {
		dst.RecordingURL = src.RecordingURL
	}
	if dst.RecordingFormat == "" {
		dst.RecordingFormat = src.RecordingFormat
	}
	if dst.Metadata == "" {
		dst.Metadata = src.Metadata
	}
}

func mapVoIPStatus(state core.CallState) string {
	switch state {
	case core.CallStateRinging, core.CallStateIncomingRinging:
		return domainCall.StatusRinging
	case core.CallStateConnecting:
		return domainCall.StatusConnecting
	case core.CallStateActive:
		return domainCall.StatusActive
	case core.CallStateEnded:
		return domainCall.StatusEnded
	default:
		return string(state)
	}
}

func normalizeCallPhone(phone string) string {
	phone = strings.TrimSpace(strings.TrimPrefix(phone, "+"))
	var b strings.Builder
	for _, ch := range phone {
		if ch >= '0' && ch <= '9' {
			b.WriteRune(ch)
		}
	}
	return b.String()
}

func wrapCallNode(from types.JID, inner *waBinary.Node) *waBinary.Node {
	content := []waBinary.Node{}
	if inner != nil {
		content = append(content, *inner)
	}
	return &waBinary.Node{
		Tag:     "call",
		Attrs:   waBinary.Attrs{"from": from},
		Content: content,
	}
}

func callIDFromNode(node *waBinary.Node) string {
	info := signaling.ExtractNodeInfo(node)
	if info == nil {
		return ""
	}
	return info.CallID
}
