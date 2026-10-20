package call

import "context"

type ICallUsecase interface {
	RejectCall(ctx context.Context, callerJID string, callID string) error
	// StartCall places a beta 1:1 audio call and returns the WhatsApp call id.
	StartCall(ctx context.Context, request StartCallRequest) (StartCallResponse, error)
	// AnswerCall accepts a ringing beta inbound call, optionally recording it.
	AnswerCall(ctx context.Context, callID string, recordPath string) error
	// HangupCall ends an active beta call.
	HangupCall(ctx context.Context, callID string) error
	// AttachAudio bridges a live call to a PCM stream: mic frames are sent to
	// the peer and the peer's audio is written to onPeerAudio.
	AttachAudio(
		ctx context.Context,
		callID string,
		mic <-chan []byte,
		onPeerAudio func([]float32),
	) error
	// WatchCallEnd runs fn when the call ends, immediately if it is already gone.
	WatchCallEnd(ctx context.Context, callID string, fn func()) error
}

type RejectCallRequest struct {
	CallerJID string `json:"caller_jid" form:"caller_jid"`
	CallID    string `json:"call_id" form:"call_id"`
}

// StartCallRequest places a beta voice call. AudioFilePath is optional: when set,
// that .mp3/.wav file is streamed to the peer once the call is answered.
// RecordPath is optional: when set, the peer's audio is saved to that .wav file.
type StartCallRequest struct {
	Target        string `json:"target" form:"target"`
	AudioFilePath string `json:"audio_file_path" form:"audio_file_path"`
	RecordPath    string `json:"record_path" form:"record_path"`
}

type StartCallResponse struct {
	CallID string `json:"call_id"`
	Target string `json:"target"`
}
