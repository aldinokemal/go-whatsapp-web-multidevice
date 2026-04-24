package utils

import (
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestDetermineMediaExtension(t *testing.T) {
	tests := []struct {
		name       string
		filename   string
		mimeType   string
		wantSuffix string
	}{
		{
			name:       "DocxFromFilename",
			filename:   "report.docx",
			mimeType:   "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			wantSuffix: ".docx",
		},
		{
			name:       "XlsxFromMime",
			filename:   "",
			mimeType:   "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			wantSuffix: ".xlsx",
		},
		{
			name:       "PptxFromMime",
			filename:   "",
			mimeType:   "application/vnd.openxmlformats-officedocument.presentationml.presentation",
			wantSuffix: ".pptx",
		},
		{
			name:       "ZipFallback",
			filename:   "",
			mimeType:   "application/zip",
			wantSuffix: ".zip",
		},
		{
			name:       "AudioOgaWithCodecsParam",
			filename:   "",
			mimeType:   "audio/ogg; codecs=opus",
			wantSuffix: ".oga",
		},
		{
			name:       "ExeFromFilename",
			filename:   "installer.exe",
			mimeType:   "application/octet-stream",
			wantSuffix: ".exe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := determineMediaExtension(tt.filename, tt.mimeType)
			if got != tt.wantSuffix {
				t.Fatalf("determineMediaExtension() = %q, want %q", got, tt.wantSuffix)
			}
		})
	}
}

func TestExtractMessageTextFromProtoContactIncludesPhone(t *testing.T) {
	displayName := "Julio"
	vcard := "BEGIN:VCARD\nVERSION:3.0\nFN:Julio\nTEL;type=CELL;waid=5511998913283:+5511998913283\nEND:VCARD"
	msg := &waE2E.Message{
		ContactMessage: &waE2E.ContactMessage{
			DisplayName: &displayName,
			Vcard:       &vcard,
		},
	}

	got := ExtractMessageTextFromProto(msg)
	want := "Contact: Julio (+5511998913283)"
	if got != want {
		t.Fatalf("ExtractMessageTextFromProto() = %q, want %q", got, want)
	}
}

func TestExtractMessageTextFromEventContactIncludesPhone(t *testing.T) {
	displayName := "Julio"
	vcard := "BEGIN:VCARD\nVERSION:3.0\nFN:Julio\nTEL;type=CELL;waid=5511998913283:+5511998913283\nEND:VCARD"
	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat: types.NewJID("5511998913283", types.DefaultUserServer),
			},
			ID:        "MSG123",
			Timestamp: time.Date(2026, time.April, 24, 10, 0, 0, 0, time.UTC),
		},
		Message: &waE2E.Message{
			ContactMessage: &waE2E.ContactMessage{
				DisplayName: &displayName,
				Vcard:       &vcard,
			},
		},
	}

	got := ExtractMessageTextFromEvent(evt)
	want := "👤 Julio (+5511998913283)"
	if got != want {
		t.Fatalf("ExtractMessageTextFromEvent() = %q, want %q", got, want)
	}
}
