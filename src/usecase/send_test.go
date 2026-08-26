package usecase

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

func writeOrientedJPEG(t *testing.T, path string, orientation byte) {
	t.Helper()
	var encoded bytes.Buffer
	require.NoError(t, jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 3)), &jpeg.Options{Quality: 100}))

	// Minimal little-endian EXIF APP1 segment containing only Orientation.
	exif := []byte{
		0xff, 0xe1, 0x00, 0x22,
		'E', 'x', 'i', 'f', 0x00, 0x00,
		'I', 'I', 0x2a, 0x00, 0x08, 0x00, 0x00, 0x00,
		0x01, 0x00,
		0x12, 0x01, 0x03, 0x00, 0x01, 0x00, 0x00, 0x00,
		orientation, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}
	jpegData := encoded.Bytes()
	fixture := make([]byte, 0, len(jpegData)+len(exif))
	fixture = append(fixture, jpegData[:2]...)
	fixture = append(fixture, exif...)
	fixture = append(fixture, jpegData[2:]...)
	require.NoError(t, os.WriteFile(path, fixture, 0600))
}

func TestOpenImageForSendAppliesEXIFOrientationForHD(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orientation-6.jpg")
	writeOrientedJPEG(t, path, 6)

	got, err := openImageForSend(path, true)
	require.NoError(t, err)
	assert.Equal(t, 3, got.Bounds().Dx())
	assert.Equal(t, 2, got.Bounds().Dy())
}

func TestSaveProcessedImageUsesJPEGForHDPNG(t *testing.T) {
	directory := t.TempDir()
	gotPath, err := saveProcessedImage(image.NewRGBA(image.Rect(0, 0, 32, 16)), directory, "report.png", true)
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(directory, "hd-report.jpg"), gotPath)

	data, err := os.ReadFile(gotPath)
	assert.NoError(t, err)
	assert.Equal(t, "image/jpeg", http.DetectContentType(data))
}

func TestResizeImageForHDCapsLongestEdgeWithoutUpscaling(t *testing.T) {
	tests := []struct {
		name       string
		width      int
		height     int
		wantWidth  int
		wantHeight int
	}{
		{name: "landscape", width: 4000, height: 2000, wantWidth: 2560, wantHeight: 1280},
		{name: "portrait", width: 2000, height: 4000, wantWidth: 1280, wantHeight: 2560},
		{name: "small image is not upscaled", width: 1200, height: 800, wantWidth: 1200, wantHeight: 800},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resizeImageForHD(image.NewRGBA(image.Rect(0, 0, tt.width, tt.height)))
			assert.Equal(t, tt.wantWidth, got.Bounds().Dx())
			assert.Equal(t, tt.wantHeight, got.Bounds().Dy())
		})
	}
}

func TestPrepareImageForSendSelectsRequestedQuality(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 3000, 1500))
	tests := []struct {
		name          string
		compress      bool
		hd            bool
		wantWidth     int
		wantHeight    int
		wantProcessed bool
	}{
		{name: "HD overrides compression", compress: true, hd: true, wantWidth: 2560, wantHeight: 1280, wantProcessed: true},
		{name: "HD overrides original mode", compress: false, hd: true, wantWidth: 2560, wantHeight: 1280, wantProcessed: true},
		{name: "legacy compression stays unchanged", compress: true, wantWidth: 600, wantHeight: 300, wantProcessed: true},
		{name: "original mode stays unchanged", wantWidth: 3000, wantHeight: 1500, wantProcessed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, processed := prepareImageForSend(source, tt.compress, tt.hd)
			assert.Equal(t, tt.wantWidth, got.Bounds().Dx())
			assert.Equal(t, tt.wantHeight, got.Bounds().Dy())
			assert.Equal(t, tt.wantProcessed, processed)
		})
	}
}

func TestBuildHDVideoFFmpegArgsUses1280BoundingBoxWithoutUpscaling(t *testing.T) {
	want := []string{
		"-i", "input.mp4",
		"-c:v", "libx264",
		"-crf", "23",
		"-preset", "fast",
		"-vf", "scale='min(1280,iw)':'min(1280,ih)':force_original_aspect_ratio=decrease:force_divisible_by=2",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-b:a", "128k",
		"-movflags", "+faststart",
		"-y",
		"output.mp4",
	}

	assert.Equal(t, want, buildHDVideoFFmpegArgs("input.mp4", "output.mp4"))
}

func TestBuildVideoTranscodeArgsSelectsRequestedQuality(t *testing.T) {
	hdArgs := buildHDVideoFFmpegArgs("input.mp4", "output.mp4")
	legacyArgs := []string{
		"-i", "input.mp4",
		"-c:v", "libx264",
		"-crf", "28",
		"-preset", "fast",
		"-vf", "scale=720:-2",
		"-c:a", "aac",
		"-b:a", "128k",
		"-movflags", "+faststart",
		"-y",
		"output.mp4",
	}

	tests := []struct {
		name       string
		compress   bool
		hd         bool
		wantArgs   []string
		wantOutput bool
	}{
		{name: "HD overrides compression", compress: true, hd: true, wantArgs: hdArgs, wantOutput: true},
		{name: "HD overrides original mode", hd: true, wantArgs: hdArgs, wantOutput: true},
		{name: "legacy compression stays unchanged", compress: true, wantArgs: legacyArgs, wantOutput: true},
		{name: "original mode stays unchanged"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotArgs, gotOutput := buildVideoTranscodeArgs("input.mp4", "output.mp4", tt.compress, tt.hd)
			assert.Equal(t, tt.wantArgs, gotArgs)
			assert.Equal(t, tt.wantOutput, gotOutput)
		})
	}
}

func TestParseVideoMetadataUsesContainerDuration(t *testing.T) {
	metadata, err := parseVideoMetadata([]byte(`{
		"streams": [{"width": 1280, "height": 720}],
		"format": {"duration": "13.040000"}
	}`))
	assert.NoError(t, err)
	assert.Equal(t, videoMetadata{Width: 1280, Height: 720, Seconds: 13}, metadata)
}

func TestParseVideoMetadataFallsBackToStreamDuration(t *testing.T) {
	metadata, err := parseVideoMetadata([]byte(`{
		"streams": [{"width": 720, "height": 1280, "duration": "9.750000"}],
		"format": {"duration": "N/A"}
	}`))
	assert.NoError(t, err)
	assert.Equal(t, uint32(9), metadata.Seconds)
}

type replyMessageRepo struct {
	domainChatStorage.IChatStorageRepository
	message     *domainChatStorage.Message
	err         error
	gotID       string
	gotDeviceID string
}

func (r *replyMessageRepo) GetMessageByIDAndDevice(deviceID, id string) (*domainChatStorage.Message, error) {
	r.gotDeviceID = deviceID
	r.gotID = id
	return r.message, r.err
}

func TestWithoutCancelPreservesDeviceContext(t *testing.T) {
	deviceID := "6289605618749@s.whatsapp.net"
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, nil))

	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()

	storeCtx := context.WithoutCancel(cancelledCtx)
	inst, ok := whatsapp.DeviceFromContext(storeCtx)
	if !ok || inst == nil {
		t.Fatal("expected device instance to remain in detached context")
	}
	if got := inst.ID(); got != deviceID {
		t.Fatalf("expected device id %q, got %q", deviceID, got)
	}
}

type pollDefinitionRepoSpy struct {
	domainChatStorage.IChatStorageRepository
	definition *domainChatStorage.PollDefinition
	err        error
}

func (r *pollDefinitionRepoSpy) UpsertPollDefinition(definition *domainChatStorage.PollDefinition) error {
	r.definition = definition
	return r.err
}

func TestPersistSentPollDefinitionStoresReadableOptions(t *testing.T) {
	repo := &pollDefinitionRepoSpy{}
	service := serviceSend{chatStorageRepo: repo}
	deviceID := "628000@s.whatsapp.net"
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, nil))
	recipient := types.NewJID("120363000000", types.GroupServer)
	request := domainSend.PollRequest{
		Question:  "Lunch?",
		Options:   []string{"Pizza", "Sushi"},
		MaxAnswer: 1,
	}

	require.NoError(t, service.persistSentPollDefinition(ctx, recipient, "POLL-SENT-1", request))
	require.NotNil(t, repo.definition)
	assert.Equal(t, deviceID, repo.definition.DeviceID)
	assert.Equal(t, recipient.String(), repo.definition.ChatJID)
	assert.Equal(t, "POLL-SENT-1", repo.definition.PollMessageID)
	assert.Equal(t, "Lunch?", repo.definition.Question)
	assert.Equal(t, uint32(1), repo.definition.SelectableOptionCount)
	assert.Equal(t, "v1", repo.definition.Version)
	require.Len(t, repo.definition.Options, 2)
	assert.Equal(t, "Sushi", repo.definition.Options[1].Name)
	assert.NotEmpty(t, repo.definition.Options[1].Hash)
}

func TestResolveDocumentMIME(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		wantMIME string
	}{
		{
			name:     "Docx",
			filename: "document.docx",
			wantMIME: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		},
		{
			name:     "Xlsx",
			filename: "spreadsheet.xlsx",
			wantMIME: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		},
		{
			name:     "Pptx",
			filename: "presentation.pptx",
			wantMIME: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		},
		{
			name:     "Zip",
			filename: "archive.zip",
			wantMIME: "application/zip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveDocumentMIME(tt.filename, []byte("dummy"))
			if got != tt.wantMIME {
				t.Fatalf("resolveDocumentMIME() = %q, want %q", got, tt.wantMIME)
			}
		})
	}
}

func TestBuildLinkMessageText(t *testing.T) {
	tests := []struct {
		name    string
		caption string
		link    string
		want    string
	}{
		{
			name: "returns link when caption is empty",
			link: "https://example.com",
			want: "https://example.com",
		},
		{
			name:    "joins caption and link with newline",
			caption: "Check this out",
			link:    "https://example.com",
			want:    "Check this out\nhttps://example.com",
		},
		{
			name:    "ignores blank caption",
			caption: "   ",
			link:    "https://example.com",
			want:    "https://example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildLinkMessageText(tt.caption, tt.link)
			if got != tt.want {
				t.Fatalf("buildLinkMessageText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeSendErrorMapsReachoutTimelock(t *testing.T) {
	err := normalizeSendError(errors.Join(whatsmeow.ErrServerReturnedError, errors.New("server returned error 463")))

	genericErr, ok := err.(pkgError.GenericError)
	if !ok {
		t.Fatalf("expected generic error, got %T", err)
	}
	if got := genericErr.ErrCode(); got != "WA_REACHOUT_TIMELOCK" {
		t.Fatalf("expected WA_REACHOUT_TIMELOCK code, got %q", got)
	}
	if got := genericErr.StatusCode(); got != http.StatusTooManyRequests {
		t.Fatalf("expected status %d, got %d", http.StatusTooManyRequests, got)
	}
	if got := genericErr.Error(); got != string(pkgError.ErrWaReachoutTimelock) {
		t.Fatalf("unexpected error message: %q", got)
	}
}

func TestMergeReplyContextAddsQuoteFields(t *testing.T) {
	replyID := "3EB089B9D6ADD58153C561"
	repo := &replyMessageRepo{
		message: &domainChatStorage.Message{
			Sender:  "628123456789@s.whatsapp.net",
			Content: "quoted message body",
		},
	}
	service := serviceSend{chatStorageRepo: repo}
	contextInfo := &waE2E.ContextInfo{}

	deviceID := "6289605618749@s.whatsapp.net"
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, nil))
	got := service.mergeReplyContext(ctx, contextInfo, &replyID)

	if got != contextInfo {
		t.Fatal("expected existing context info to be reused")
	}
	if repo.gotID != replyID {
		t.Fatalf("expected lookup for reply ID %q, got %q", replyID, repo.gotID)
	}
	if repo.gotDeviceID != deviceID {
		t.Fatalf("expected device-scoped lookup for %q, got %q", deviceID, repo.gotDeviceID)
	}
	if got.GetStanzaID() != replyID {
		t.Fatalf("expected stanza ID %q, got %q", replyID, got.GetStanzaID())
	}
	if got.GetParticipant() != "628123456789@s.whatsapp.net" {
		t.Fatalf("unexpected participant: %q", got.GetParticipant())
	}
	if got.GetQuotedMessage().GetConversation() != "quoted message body" {
		t.Fatalf("unexpected quoted body: %q", got.GetQuotedMessage().GetConversation())
	}
}

func TestMergeReplyContextPreservesExistingContext(t *testing.T) {
	replyID := "3EB089B9D6ADD58153C561"
	repo := &replyMessageRepo{
		message: &domainChatStorage.Message{
			Sender:  "628123456789@s.whatsapp.net",
			Content: "quoted message body",
		},
	}
	service := serviceSend{chatStorageRepo: repo}
	contextInfo := &waE2E.ContextInfo{
		IsForwarded:     proto.Bool(true),
		ForwardingScore: proto.Uint32(100),
		Expiration:      proto.Uint32(3600),
		MentionedJID:    []string{"628999999999@s.whatsapp.net"},
	}

	got := service.mergeReplyContext(context.Background(), contextInfo, &replyID)

	if !got.GetIsForwarded() {
		t.Fatal("expected forwarded flag to be preserved")
	}
	if got.GetForwardingScore() != 100 {
		t.Fatalf("expected forwarding score 100, got %d", got.GetForwardingScore())
	}
	if got.GetExpiration() != 3600 {
		t.Fatalf("expected expiration 3600, got %d", got.GetExpiration())
	}
	if len(got.GetMentionedJID()) != 1 || got.GetMentionedJID()[0] != "628999999999@s.whatsapp.net" {
		t.Fatalf("expected mentioned JIDs to be preserved, got %#v", got.GetMentionedJID())
	}
	if got.GetQuotedMessage().GetConversation() != "quoted message body" {
		t.Fatalf("unexpected quoted body: %q", got.GetQuotedMessage().GetConversation())
	}
}

func TestMergeReplyContextLeavesExistingContextWhenReplyUnavailable(t *testing.T) {
	replyID := "3EB089B9D6ADD58153C561"

	tests := []struct {
		name    string
		replyID *string
		message *domainChatStorage.Message
		err     error
	}{
		{
			name: "nil reply ID",
		},
		{
			name:    "empty reply ID",
			replyID: proto.String(""),
		},
		{
			name:    "message not found",
			replyID: &replyID,
		},
		{
			name:    "lookup error",
			replyID: &replyID,
			err:     errors.New("storage unavailable"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contextInfo := &waE2E.ContextInfo{Expiration: proto.Uint32(3600)}
			service := serviceSend{chatStorageRepo: &replyMessageRepo{
				message: tt.message,
				err:     tt.err,
			}}

			got := service.mergeReplyContext(context.Background(), contextInfo, tt.replyID)

			if got != contextInfo {
				t.Fatal("expected existing context info to be reused")
			}
			if got.GetExpiration() != 3600 {
				t.Fatalf("expected expiration to remain 3600, got %d", got.GetExpiration())
			}
			if got.GetStanzaID() != "" {
				t.Fatalf("expected no stanza ID, got %q", got.GetStanzaID())
			}
			if got.GetQuotedMessage() != nil {
				t.Fatalf("expected no quoted message, got %#v", got.GetQuotedMessage())
			}
		})
	}
}

func TestSendForwardMessageNotFound(t *testing.T) {
	repo := &replyMessageRepo{}
	service := serviceSend{chatStorageRepo: repo}
	deviceID := "6289605618749@s.whatsapp.net"
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, nil))

	_, err := service.SendForward(ctx, domainSend.ForwardRequest{
		MessageID: "missing-id",
		Phone:     "628123456789@s.whatsapp.net",
	})
	if err == nil {
		t.Fatal("expected error for missing message")
	}
	if repo.gotID != "missing-id" {
		t.Fatalf("expected lookup for missing-id, got %q", repo.gotID)
	}
	if repo.gotDeviceID != deviceID {
		t.Fatalf("expected device-scoped lookup for %q, got %q", deviceID, repo.gotDeviceID)
	}
}

func TestForwardDurationOptionExplicitZero(t *testing.T) {
	zero := 0
	got := forwardDurationOption(serviceSend{}, domainSend.ForwardRequest{
		Phone:    "628123456789@s.whatsapp.net",
		Duration: &zero,
	})
	if got == nil || *got != 0 {
		t.Fatalf("expected explicit duration 0 to be honored, got %v", got)
	}
}

func TestSendForwardUnsupportedType(t *testing.T) {
	repo := &replyMessageRepo{
		message: &domainChatStorage.Message{
			MediaType: "call",
			Content:   "incoming call",
		},
	}
	service := serviceSend{chatStorageRepo: repo}
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance("6289605618749@s.whatsapp.net", nil, nil))

	_, err := service.SendForward(ctx, domainSend.ForwardRequest{
		MessageID: "call-msg-id",
		Phone:     "628123456789@s.whatsapp.net",
	})
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
	genericErr, ok := err.(pkgError.GenericError)
	if !ok {
		t.Fatalf("expected validation error, got %T: %v", err, err)
	}
	if genericErr.Error() != utils.ErrUnsupportedForwardType {
		t.Fatalf("error = %q, want %q", genericErr.Error(), utils.ErrUnsupportedForwardType)
	}
}
