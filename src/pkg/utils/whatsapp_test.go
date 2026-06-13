package utils

import (
	"strings"
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
			name:  "FoldedLine",
			vcard: "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Julio\r\nTEL;type=CELL;waid=5511998913283:\r\n +5511998913283\r\nEND:VCARD",
			want:  "+5511998913283",
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
		{name: "SingleWhitespaceOnly", dName: " ", phone: " ", plural: false, want: "Contact shared"},
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
	phoneVCard := "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Alice\r\nTEL;type=Mobile:\r\n +62 812 3456 7890\r\nEND:VCARD"

	tests := []struct {
		name string
		msg  *waE2E.Message
		want string
	}{
		{
			name: "NameAndPhone",
			msg: &waE2E.Message{
				ContactMessage: &waE2E.ContactMessage{
					DisplayName: strPtr("Alice"),
					Vcard:       strPtr(phoneVCard),
				},
			},
			want: "Contact: Alice (+62 812 3456 7890)",
		},
		{
			name: "NameOnly",
			msg: &waE2E.Message{
				ContactMessage: &waE2E.ContactMessage{
					DisplayName: strPtr("Alice"),
					Vcard:       strPtr(""),
				},
			},
			want: "Contact: Alice",
		},
		{
			name: "PhoneOnly",
			msg: &waE2E.Message{
				ContactMessage: &waE2E.ContactMessage{
					DisplayName: strPtr(""),
					Vcard:       strPtr(phoneVCard),
				},
			},
			want: "Contact: +62 812 3456 7890",
		},
		{
			name: "Neither",
			msg: &waE2E.Message{
				ContactMessage: &waE2E.ContactMessage{
					DisplayName: strPtr(""),
					Vcard:       strPtr(""),
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

func strPtr(value string) *string {
	return &value
}

// TestDeriveDirectPath verifies the fix for the "no url present" download bug.
//
// whatsmeow's Client.Download downloads ONLY from GetDirectPath() and returns
// ErrNoURLPresent when DirectPath is empty — it ignores the URL entirely.
// DownloadMediaWithPath then rejects any directPath that does not start with
// "/". Chat storage only persists the full CDN url (no direct_path column), so
// the download paths reconstruct directPath from the stored url via
// DeriveDirectPath, which returns RequestURI() (path+"?"+query) when usable and
// "" otherwise.
//
// Every want value is a hardcoded literal, never recomputed via net/url, so the
// test pins behaviour and cannot pass as a tautology.
func TestDeriveDirectPath(t *testing.T) {
	tests := []struct {
		name   string
		rawURL string
		want   string
		wantOK bool // true => a usable directPath (starts with "/") is expected
	}{
		{
			name:   "EncVPathWithQuery",
			rawURL: "https://mmg.whatsapp.net/v/t62.7118-24/12345_67890_n.enc?ccb=11-4&oh=t1&oe=t2&mms3=true",
			want:   "/v/t62.7118-24/12345_67890_n.enc?ccb=11-4&oh=t1&oe=t2&mms3=true",
			wantOK: true,
		},
		{
			name:   "O1VPathWithQuery",
			rawURL: "https://mmg.whatsapp.net/o1/v/t24/f2/m231/AQabc123?ccb=9-4&oh=h1&oe=e1",
			want:   "/o1/v/t24/f2/m231/AQabc123?ccb=9-4&oh=h1&oe=e1",
			wantOK: true,
		},
		{
			name:   "NoQueryString",
			rawURL: "https://mmg.whatsapp.net/v/t62.7118-24/no_query.enc",
			want:   "/v/t62.7118-24/no_query.enc",
			wantOK: true,
		},
		{
			// Empty input must NOT produce the deceptive "/" that
			// (&url.URL{}).RequestURI() returns — it must fall back to "".
			name:   "EmptyURL",
			rawURL: "",
			want:   "",
			wantOK: false,
		},
		{
			// "garbage" parses without error but RequestURI() == "garbage",
			// which lacks a leading slash, so the helper rejects it.
			name:   "GarbageWithoutScheme",
			rawURL: "garbage",
			want:   "",
			wantOK: false,
		},
		{
			// A scheme-less host/path is treated as a relative path by net/url
			// (RequestURI == "mmg.whatsapp.net/v/path?x=1"), no leading slash.
			name:   "SchemeLessHostAndPath",
			rawURL: "mmg.whatsapp.net/v/path?x=1",
			want:   "",
			wantOK: false,
		},
		{
			name:   "UnparseableURL",
			rawURL: "://nohost",
			want:   "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeriveDirectPath(tt.rawURL)

			if got != tt.want {
				t.Fatalf("DeriveDirectPath(%q) = %q, want %q", tt.rawURL, got, tt.want)
			}

			// Enforce whatsmeow's DownloadMediaWithPath contract: a usable
			// directPath must start with "/".
			if tt.wantOK {
				if !strings.HasPrefix(got, "/") {
					t.Fatalf("DeriveDirectPath(%q) = %q, expected a directPath starting with %q", tt.rawURL, got, "/")
				}
			} else if got != "" {
				t.Fatalf("DeriveDirectPath(%q) = %q, expected empty fallback for unusable input", tt.rawURL, got)
			}
		})
	}
}
