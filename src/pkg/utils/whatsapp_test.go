package utils

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
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

func TestExtractPhoneFromVCard(t *testing.T) {
	vcard := "BEGIN:VCARD\nVERSION:3.0\nN:;Alice;;;\nFN:Alice\nTEL;type=Mobile:+62 812 3456 7890\nEND:VCARD"

	got := extractPhoneFromVCard(vcard)
	if got != "+62 812 3456 7890" {
		t.Fatalf("extractPhoneFromVCard() = %q, want %q", got, "+62 812 3456 7890")
	}
}

func TestExtractMessageTextFromProtoContactMessage(t *testing.T) {
	name := "Alice"
	vcard := "BEGIN:VCARD\nVERSION:3.0\nN:;Alice;;;\nFN:Alice\nTEL;type=Mobile:+62 812 3456 7890\nEND:VCARD"
	msg := &waE2E.Message{
		ContactMessage: &waE2E.ContactMessage{
			DisplayName: &name,
			Vcard:       &vcard,
		},
	}

	got := ExtractMessageTextFromProto(msg)
	want := "Contact: Alice (+62 812 3456 7890)"
	if got != want {
		t.Fatalf("ExtractMessageTextFromProto() = %q, want %q", got, want)
	}
}
