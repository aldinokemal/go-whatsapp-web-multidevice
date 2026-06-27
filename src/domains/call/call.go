package call

import (
	"context"
	"time"
)

const (
	StatusRinging    = "ringing"
	StatusConnecting = "connecting"
	StatusActive     = "active"
	StatusEnded      = "ended"
	StatusFailed     = "failed"

	DirectionInbound  = "inbound"
	DirectionOutbound = "outbound"

	MediaTypeAudio = "audio"
)

type StartCallRequest struct {
	Phone string `json:"phone"`
	Video bool   `json:"video,omitempty"`
}

type CallIDRequest struct {
	CallID string `json:"call_id" uri:"call_id"`
}

type WebRTCRequest struct {
	CallID   string `json:"call_id" uri:"call_id"`
	SDPOffer string `json:"sdp_offer"`
}

type WebRTCResponse struct {
	CallID    string `json:"call_id"`
	SDPAnswer string `json:"sdp_answer"`
}

type CallInfo struct {
	DeviceID  string    `json:"device_id"`
	CallID    string    `json:"call_id"`
	PeerJID   string    `json:"peer_jid"`
	Direction string    `json:"direction"`
	Status    string    `json:"status"`
	MediaType string    `json:"media_type"`
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
	EndReason string    `json:"end_reason,omitempty"`
	Metadata  string    `json:"metadata,omitempty"`
}

type StartCallResponse struct {
	Call CallInfo `json:"call"`
}

type GenericResponse struct {
	Status string   `json:"status"`
	Call   CallInfo `json:"call"`
}

type ListCallsResponse struct {
	Data []CallInfo `json:"data"`
}

type ICallUsecase interface {
	StartCall(ctx context.Context, request StartCallRequest) (StartCallResponse, error)
	AcceptCall(ctx context.Context, request CallIDRequest) (GenericResponse, error)
	RejectCall(ctx context.Context, request CallIDRequest) (GenericResponse, error)
	EndCall(ctx context.Context, request CallIDRequest) (GenericResponse, error)
	ExchangeWebRTC(ctx context.Context, request WebRTCRequest) (WebRTCResponse, error)
	GetCall(ctx context.Context, request CallIDRequest) (CallInfo, error)
	ListCalls(ctx context.Context) (ListCallsResponse, error)
}
