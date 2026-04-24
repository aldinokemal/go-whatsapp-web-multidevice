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

func TestFormatContactMessageTextEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		prefix      string
		fallback    string
		displayName string
		vcard       string
		want        string
	}{
		{
			name:        "crlf vcard with name and phone",
			prefix:      "Contact: ",
			fallback:    "Contact shared",
			displayName: "Julio",
			vcard:       "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Julio\r\nTEL;type=CELL;waid=5511998913283:+5511998913283\r\nEND:VCARD",
			want:        "Contact: Julio (+5511998913283)",
		},
		{
			name:        "name only",
			prefix:      "Contact: ",
			fallback:    "Contact shared",
			displayName: "Julio",
			vcard:       "BEGIN:VCARD\nVERSION:3.0\nFN:Julio\nEND:VCARD",
			want:        "Contact: Julio",
		},
		{
			name:        "phone only",
			prefix:      "Contact: ",
			fallback:    "Contact shared",
			displayName: "",
			vcard:       "BEGIN:VCARD\nVERSION:3.0\nTEL;type=CELL;waid=5511998913283:+5511998913283\nEND:VCARD",
			want:        "Contact: +5511998913283",
		},
		{
			name:        "no name and no phone",
			prefix:      "Contact: ",
			fallback:    "Contact shared",
			displayName: "",
			vcard:       "BEGIN:VCARD\nVERSION:3.0\nEND:VCARD",
			want:        "Contact shared",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatContactMessageText(tt.prefix, tt.fallback, tt.displayName, tt.vcard)
			if got != tt.want {
				t.Fatalf("formatContactMessageText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractMessageTextFromProtoContactBranches(t *testing.T) {
	phoneVCard := "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Julio\r\nTEL;type=CELL;waid=5511998913283:\r\n +5511998913283\r\nEND:VCARD"
	emptyVCard := ""
	emptyName := ""

	tests := []struct {
		name string
		msg  *waE2E.Message
		want string
	}{
		{
			name: "both name and phone",
			msg: &waE2E.Message{
				ContactMessage: &waE2E.ContactMessage{
					DisplayName: strPtr("Julio"),
					Vcard:       strPtr(phoneVCard),
				},
			},
			want: "Contact: Julio (+5511998913283)",
		},
		{
			name: "name only",
			msg: &waE2E.Message{
				ContactMessage: &waE2E.ContactMessage{
					DisplayName: strPtr("Julio"),
					Vcard:       strPtr(emptyVCard),
				},
			},
			want: "Contact: Julio",
		},
		{
			name: "phone only",
			msg: &waE2E.Message{
				ContactMessage: &waE2E.ContactMessage{
					DisplayName: &emptyName,
					Vcard:       strPtr(phoneVCard),
				},
			},
			want: "Contact: +5511998913283",
		},
		{
			name: "neither",
			msg: &waE2E.Message{
				ContactMessage: &waE2E.ContactMessage{
					DisplayName: &emptyName,
					Vcard:       strPtr(emptyVCard),
				},
			},
			want: "Contact shared",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractMessageTextFromProto(tt.msg)
			if got != tt.want {
				t.Fatalf("ExtractMessageTextFromProto() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractMessageTextFromEventContactBranches(t *testing.T) {
	phoneVCard := "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Julio\r\nTEL;type=CELL;waid=5511998913283:\r\n +5511998913283\r\nEND:VCARD"
	emptyVCard := ""
	emptyName := ""

	tests := []struct {
		name string
		evt  *events.Message
		want string
	}{
		{
			name: "both name and phone",
			evt: &events.Message{
				Info: types.MessageInfo{
					MessageSource: types.MessageSource{
						Chat: types.NewJID("5511998913283", types.DefaultUserServer),
					},
					ID:        "MSG124",
					Timestamp: time.Date(2026, time.April, 24, 10, 0, 0, 0, time.UTC),
				},
				Message: &waE2E.Message{
					ContactMessage: &waE2E.ContactMessage{
						DisplayName: strPtr("Julio"),
						Vcard:       strPtr(phoneVCard),
					},
				},
			},
			want: "👤 Julio (+5511998913283)",
		},
		{
			name: "name only",
			evt: &events.Message{
				Info: types.MessageInfo{
					MessageSource: types.MessageSource{
						Chat: types.NewJID("5511998913283", types.DefaultUserServer),
					},
					ID:        "MSG125",
					Timestamp: time.Date(2026, time.April, 24, 10, 0, 0, 0, time.UTC),
				},
				Message: &waE2E.Message{
					ContactMessage: &waE2E.ContactMessage{
						DisplayName: strPtr("Julio"),
						Vcard:       strPtr(emptyVCard),
					},
				},
			},
			want: "👤 Julio",
		},
		{
			name: "phone only",
			evt: &events.Message{
				Info: types.MessageInfo{
					MessageSource: types.MessageSource{
						Chat: types.NewJID("5511998913283", types.DefaultUserServer),
					},
					ID:        "MSG126",
					Timestamp: time.Date(2026, time.April, 24, 10, 0, 0, 0, time.UTC),
				},
				Message: &waE2E.Message{
					ContactMessage: &waE2E.ContactMessage{
						DisplayName: &emptyName,
						Vcard:       strPtr(phoneVCard),
					},
				},
			},
			want: "👤 +5511998913283",
		},
		{
			name: "neither",
			evt: &events.Message{
				Info: types.MessageInfo{
					MessageSource: types.MessageSource{
						Chat: types.NewJID("5511998913283", types.DefaultUserServer),
					},
					ID:        "MSG127",
					Timestamp: time.Date(2026, time.April, 24, 10, 0, 0, 0, time.UTC),
				},
				Message: &waE2E.Message{
					ContactMessage: &waE2E.ContactMessage{
						DisplayName: &emptyName,
						Vcard:       strPtr(emptyVCard),
					},
				},
			},
			want: "👤 Contact",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractMessageTextFromEvent(tt.evt)
			if got != tt.want {
				t.Fatalf("ExtractMessageTextFromEvent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractPhoneFromVCardHandlesCRLFAndFoldedLines(t *testing.T) {
	vcard := "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Julio\r\nTEL;type=CELL;waid=5511998913283:\r\n +5511998913283\r\nEND:VCARD"

	got := ExtractPhoneFromVCard(vcard)
	want := "+5511998913283"
	if got != want {
		t.Fatalf("ExtractPhoneFromVCard() = %q, want %q", got, want)
	}
}

func strPtr(value string) *string {
	return &value
}
