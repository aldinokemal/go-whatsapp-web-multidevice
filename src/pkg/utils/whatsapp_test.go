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

func TestFormatLocationSummary(t *testing.T) {
	tests := []struct {
		name    string
		locName string
		address string
		lat     float64
		long    float64
		want    string
	}{
		{
			name:    "NameAndAddress",
			locName: "Central Tower",
			address: "1 Example Street",
			lat:     -6.175392,
			long:    106.827153,
			want:    "Central Tower — 1 Example Street — https://maps.google.com/?q=-6.175392,106.827153",
		},
		{
			name:    "NameOnly",
			locName: "Central Tower",
			address: "",
			lat:     1.5,
			long:    2.5,
			want:    "Central Tower — https://maps.google.com/?q=1.5,2.5",
		},
		{
			name:    "CoordinatesOnly",
			locName: "",
			address: "",
			lat:     -6.2,
			long:    106.8,
			want:    "https://maps.google.com/?q=-6.2,106.8",
		},
		{
			name:    "WhitespaceNameAndAddressTrimmed",
			locName: "  ",
			address: "  ",
			lat:     0,
			long:    0,
			want:    "https://maps.google.com/?q=0,0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatLocationSummary(tt.locName, tt.address, tt.lat, tt.long)
			if got != tt.want {
				t.Fatalf("FormatLocationSummary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractMessageTextFromProtoLocationMessage(t *testing.T) {
	tests := []struct {
		name string
		msg  *waE2E.Message
		want string
	}{
		{
			name: "LocationWithNameAndAddress",
			msg: &waE2E.Message{
				LocationMessage: &waE2E.LocationMessage{
					Name:             strPtr("Central Tower"),
					Address:          strPtr("1 Example Street"),
					DegreesLatitude:  f64Ptr(-6.175392),
					DegreesLongitude: f64Ptr(106.827153),
				},
			},
			want: "Central Tower — 1 Example Street — https://maps.google.com/?q=-6.175392,106.827153",
		},
		{
			name: "LocationCoordinatesOnly",
			msg: &waE2E.Message{
				LocationMessage: &waE2E.LocationMessage{
					DegreesLatitude:  f64Ptr(-6.2),
					DegreesLongitude: f64Ptr(106.8),
				},
			},
			want: "https://maps.google.com/?q=-6.2,106.8",
		},
		{
			name: "LiveLocationWithCaption",
			msg: &waE2E.Message{
				LiveLocationMessage: &waE2E.LiveLocationMessage{
					Caption:          strPtr("on the move"),
					DegreesLatitude:  f64Ptr(-6.2),
					DegreesLongitude: f64Ptr(106.8),
				},
			},
			want: "on the move — https://maps.google.com/?q=-6.2,106.8",
		},
		{
			name: "LiveLocationCoordinatesOnly",
			msg: &waE2E.Message{
				LiveLocationMessage: &waE2E.LiveLocationMessage{
					DegreesLatitude:  f64Ptr(1.5),
					DegreesLongitude: f64Ptr(2.5),
				},
			},
			want: "https://maps.google.com/?q=1.5,2.5",
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

func TestExtractMessageTextFromProtoExtendedText(t *testing.T) {
	tests := []struct {
		name string
		msg  *waE2E.Message
		want string
	}{
		{
			name: "TextPresentWins",
			msg: &waE2E.Message{
				ExtendedTextMessage: &waE2E.ExtendedTextMessage{
					Text:        strPtr("look at this"),
					MatchedText: strPtr("https://example.com"),
				},
			},
			want: "look at this",
		},
		{
			name: "EmptyTextFallsBackToMatchedText",
			msg: &waE2E.Message{
				ExtendedTextMessage: &waE2E.ExtendedTextMessage{
					Text:        strPtr(""),
					MatchedText: strPtr("https://example.com"),
				},
			},
			want: "https://example.com",
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

func f64Ptr(value float64) *float64 {
	return &value
}
