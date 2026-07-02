package core

type CallState string

const (
	CallStateInitiating      CallState = "initiating"
	CallStateRinging         CallState = "ringing"
	CallStateIncomingRinging CallState = "incoming_ringing"
	CallStateConnecting      CallState = "connecting"
	CallStateActive          CallState = "active"
	CallStateOnHold          CallState = "on_hold"
	CallStateEnded           CallState = "ended"
)

type CallDirection string

const (
	CallDirectionOutgoing CallDirection = "outgoing"
	CallDirectionIncoming CallDirection = "incoming"
)

type CallMediaType string

const (
	CallMediaTypeAudio CallMediaType = "audio"
	CallMediaTypeVideo CallMediaType = "video"
)

type EndCallReason string

const (
	EndCallReasonUserEnded    EndCallReason = "user_ended"
	EndCallReasonDeclined     EndCallReason = "declined"
	EndCallReasonTimeout      EndCallReason = "timeout"
	EndCallReasonBusy         EndCallReason = "busy"
	EndCallReasonCancelled    EndCallReason = "cancelled"
	EndCallReasonFailed       EndCallReason = "failed"
	EndCallReasonDoNotDisturb EndCallReason = "do_not_disturb"
	EndCallReasonUnknown      EndCallReason = "unknown"
)

const (
	PayloadTypeWhatsAppOpus = 120
)

const (
	SRTPSendAuthTagLen = 4
	SRTPRecvAuthTagLen = 4
	SRTPAuthTagLen     = 4
)

const (
	SRTPLabelEncryption = 0x00
	SRTPLabelAuth       = 0x01
	SRTPLabelSalt       = 0x02
)

const WARelayPort = 3480

const WADTLSFingerprint = "sha-256 F9:CA:0C:98:A3:CC:71:D6:42:CE:5A:E2:53:D2:15:20:D3:1B:BA:D8:57:A4:F0:AF:BE:0B:FB:F3:6B:0C:A0:68"

type SrtpKeyingMaterial struct {
	MasterKey  []byte
	MasterSalt []byte
}

type RelayEndpoint struct {
	IP           string
	Port         int
	Token        string
	AuthToken    string
	RawAuthToken []byte
	RawToken     []byte
	Key          string
	RelayID      int
	Protocol     int
	C2RRtt       *int
	RelayName    string
	AddressBytes []byte
	AuthTokenID  string
}

type RelayData struct {
	Endpoints       []RelayEndpoint
	ParticipantJids []string
	UUID            string
	SelfPid         *int
	PeerPid         *int
	HbhKey          []byte
}

type AudioEngineConfig struct {
	SampleRate         int
	CaptureChunkSize   int
	PlaybackOutputSize int
	MaxBufferSize      int
	IntervalMs         int
}

var DefaultAudioConfig = AudioEngineConfig{
	SampleRate:         16000,
	CaptureChunkSize:   320,
	PlaybackOutputSize: 256,
	MaxBufferSize:      1600,
	IntervalMs:         20,
}
