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
	tests := []struct {
		name  string
		vcard string
		want  string
	}{
		{
			name:  "LFEndings",
			vcard: "BEGIN:VCARD\nVERSION:3.0\nFN:Alice\nTEL;type=Mobile:+62 812 3456 7890\nEND:VCARD",
			want:  "+62 812 3456 7890",
		},
		{
			name:  "CRLFEndings",
			vcard: "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Bob\r\nTEL:+1 555 0100\r\nEND:VCARD",
			want:  "+1 555 0100",
		},
		{
			name:  "NoTelLine",
			vcard: "BEGIN:VCARD\nVERSION:3.0\nFN:Carol\nEND:VCARD",
			want:  "",
		},
		{
			name:  "Empty",
			vcard: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractPhoneFromVCard(tt.vcard)
			if got != tt.want {
				t.Fatalf("ExtractPhoneFromVCard() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatContactSummary(t *testing.T) {
	tests := []struct {
		name   string
		dName  string
		phone  string
		plural bool
		want   string
	}{
		{name: "SingleNameAndPhone", dName: "Alice", phone: "+62 812", plural: false, want: "Contact: Alice (+62 812)"},
		{name: "SingleNameOnly", dName: "Alice", phone: "", plural: false, want: "Contact: Alice"},
		{name: "SinglePhoneOnly", dName: "", phone: "+62 812", plural: false, want: "Contact: +62 812"},
		{name: "SingleEmpty", dName: "", phone: "", plural: false, want: "Contact shared"},
		{name: "PluralNameAndPhone", dName: "Alice", phone: "+62 812", plural: true, want: "Contacts: Alice (+62 812)"},
		{name: "PluralEmpty", dName: "", phone: "", plural: true, want: "Contacts shared"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatContactSummary(tt.dName, tt.phone, tt.plural)
			if got != tt.want {
				t.Fatalf("FormatContactSummary() = %q, want %q", got, tt.want)
			}
		})
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

func TestExtractMessageTextFromProtoContactsArrayMessage(t *testing.T) {
	bob := "Bob"
	bobVcard := "BEGIN:VCARD\nVERSION:3.0\nFN:Bob\nTEL:+1 555 0100\nEND:VCARD"
	carol := "Carol"
	carolVcard := "BEGIN:VCARD\nVERSION:3.0\nFN:Carol\nTEL:+1 555 0200\nEND:VCARD"

	tests := []struct {
		name string
		msg  *waE2E.Message
		want string
	}{
		{
			name: "FirstContactWithNameAndPhone",
			msg: &waE2E.Message{
				ContactsArrayMessage: &waE2E.ContactsArrayMessage{
					Contacts: []*waE2E.ContactMessage{
						{DisplayName: &bob, Vcard: &bobVcard},
						{DisplayName: &carol, Vcard: &carolVcard},
					},
				},
			},
			want: "Contacts: Bob (+1 555 0100)",
		},
		{
			name: "EmptyContactsArray",
			msg: &waE2E.Message{
				ContactsArrayMessage: &waE2E.ContactsArrayMessage{Contacts: nil},
			},
			want: "Contacts shared",
		},
		{
			name: "FirstContactEmpty",
			msg: &waE2E.Message{
				ContactsArrayMessage: &waE2E.ContactsArrayMessage{
					Contacts: []*waE2E.ContactMessage{{}},
				},
			},
			want: "Contacts shared",
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
