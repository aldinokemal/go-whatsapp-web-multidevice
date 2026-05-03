package utils

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
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
	vcard := "BEGIN:VCARD\nVERSION:3.0\nFN:Alice\nTEL;type=CELL;waid=628123456789:+628123456789\nEND:VCARD"

	got := ExtractPhoneFromVCard(vcard)
	if got != "+628123456789" {
		t.Fatalf("ExtractPhoneFromVCard() = %q, want %q", got, "+628123456789")
	}
}

func TestFormatContactText(t *testing.T) {
	got := FormatContactText("Alice", "+628123456789")
	want := "Contact: Alice (+628123456789)"
	if got != want {
		t.Fatalf("FormatContactText() = %q, want %q", got, want)
	}
}

func TestFormatContactsText(t *testing.T) {
	got := FormatContactsText("Alice", "+628123456789", 2)
	want := "Contacts: Alice (+628123456789) +2 more"
	if got != want {
		t.Fatalf("FormatContactsText() = %q, want %q", got, want)
	}
}

func TestExtractMessageTextFromProtoContact(t *testing.T) {
	msg := &waE2E.Message{
		ContactMessage: &waE2E.ContactMessage{
			DisplayName: proto.String("Alice"),
			Vcard:       proto.String("BEGIN:VCARD\nTEL;type=CELL;waid=628123456789:+628123456789\nEND:VCARD"),
		},
	}

	got := ExtractMessageTextFromProto(msg)
	want := "Contact: Alice (+628123456789)"
	if got != want {
		t.Fatalf("ExtractMessageTextFromProto() = %q, want %q", got, want)
	}
}

func TestExtractMessageTextFromProtoContactsArray(t *testing.T) {
	msg := &waE2E.Message{
		ContactsArrayMessage: &waE2E.ContactsArrayMessage{
			Contacts: []*waE2E.ContactMessage{
				{
					DisplayName: proto.String("Alice"),
					Vcard:       proto.String("BEGIN:VCARD\nTEL;type=CELL;waid=628123456789:+628123456789\nEND:VCARD"),
				},
				{
					DisplayName: proto.String("Bob"),
					Vcard:       proto.String("BEGIN:VCARD\nTEL;type=CELL;waid=628987654321:+628987654321\nEND:VCARD"),
				},
			},
		},
	}

	got := ExtractMessageTextFromProto(msg)
	want := "Contacts: Alice (+628123456789) +1 more"
	if got != want {
		t.Fatalf("ExtractMessageTextFromProto() = %q, want %q", got, want)
	}
}

func TestExtractMessageTextFromEventContact(t *testing.T) {
	evt := &events.Message{
		Message: &waE2E.Message{
			ContactMessage: &waE2E.ContactMessage{
				DisplayName: proto.String("Alice"),
				Vcard:       proto.String("BEGIN:VCARD\nTEL;type=CELL;waid=628123456789:+628123456789\nEND:VCARD"),
			},
		},
	}

	got := ExtractMessageTextFromEvent(evt)
	want := "👤 Contact: Alice (+628123456789)"
	if got != want {
		t.Fatalf("ExtractMessageTextFromEvent() = %q, want %q", got, want)
	}
}

func TestExtractMessageTextFromEventContactsArray(t *testing.T) {
	evt := &events.Message{
		Message: &waE2E.Message{
			ContactsArrayMessage: &waE2E.ContactsArrayMessage{
				Contacts: []*waE2E.ContactMessage{
					{
						DisplayName: proto.String("Alice"),
						Vcard:       proto.String("BEGIN:VCARD\nTEL;type=CELL;waid=628123456789:+628123456789\nEND:VCARD"),
					},
					{
						DisplayName: proto.String("Bob"),
						Vcard:       proto.String("BEGIN:VCARD\nTEL;type=CELL;waid=628987654321:+628987654321\nEND:VCARD"),
					},
				},
			},
		},
	}

	got := ExtractMessageTextFromEvent(evt)
	want := "👥 Contacts: Alice (+628123456789) +1 more"
	if got != want {
		t.Fatalf("ExtractMessageTextFromEvent() = %q, want %q", got, want)
	}
}
