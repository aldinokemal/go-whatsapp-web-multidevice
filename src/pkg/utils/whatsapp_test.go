package utils

import (
	"bytes"
	"testing"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"go.mau.fi/whatsmeow/proto/waE2E"
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

func TestExtractMediaInfoIncludesDirectPath(t *testing.T) {
	mediaKey := []byte("media-key")
	fileSHA256 := []byte("file-sha")
	fileEncSHA256 := []byte("file-enc-sha")
	fileLength := uint64(1234)
	mediaURL := "https://mmg.whatsapp.net/v/t62.7118-24/media.enc?ccb=11-4"
	directPath := "/v/t62.7118-24/media.enc?ccb=11-4"

	tests := []struct {
		name     string
		msg      *waE2E.Message
		wantType string
	}{
		{
			name: "Image",
			msg: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
				URL:           proto.String(mediaURL),
				DirectPath:    proto.String(directPath),
				MediaKey:      mediaKey,
				FileSHA256:    fileSHA256,
				FileEncSHA256: fileEncSHA256,
				FileLength:    proto.Uint64(fileLength),
			}},
			wantType: "image",
		},
		{
			name: "Video",
			msg: &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
				URL:           proto.String(mediaURL),
				DirectPath:    proto.String(directPath),
				MediaKey:      mediaKey,
				FileSHA256:    fileSHA256,
				FileEncSHA256: fileEncSHA256,
				FileLength:    proto.Uint64(fileLength),
			}},
			wantType: "video",
		},
		{
			name: "VideoNote",
			msg: &waE2E.Message{PtvMessage: &waE2E.VideoMessage{
				URL:           proto.String(mediaURL),
				DirectPath:    proto.String(directPath),
				MediaKey:      mediaKey,
				FileSHA256:    fileSHA256,
				FileEncSHA256: fileEncSHA256,
				FileLength:    proto.Uint64(fileLength),
			}},
			wantType: "video_note",
		},
		{
			name: "Audio",
			msg: &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
				URL:           proto.String(mediaURL),
				DirectPath:    proto.String(directPath),
				MediaKey:      mediaKey,
				FileSHA256:    fileSHA256,
				FileEncSHA256: fileEncSHA256,
				FileLength:    proto.Uint64(fileLength),
			}},
			wantType: "audio",
		},
		{
			name: "Document",
			msg: &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
				URL:           proto.String(mediaURL),
				DirectPath:    proto.String(directPath),
				MediaKey:      mediaKey,
				FileSHA256:    fileSHA256,
				FileEncSHA256: fileEncSHA256,
				FileLength:    proto.Uint64(fileLength),
				FileName:      proto.String("report.pdf"),
			}},
			wantType: "document",
		},
		{
			name: "Sticker",
			msg: &waE2E.Message{StickerMessage: &waE2E.StickerMessage{
				URL:           proto.String(mediaURL),
				DirectPath:    proto.String(directPath),
				MediaKey:      mediaKey,
				FileSHA256:    fileSHA256,
				FileEncSHA256: fileEncSHA256,
				FileLength:    proto.Uint64(fileLength),
			}},
			wantType: "sticker",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, _, gotURL, gotDirectPath, gotMediaKey, gotFileSHA256, gotFileEncSHA256, gotFileLength := ExtractMediaInfo(tt.msg)
			if gotType != tt.wantType {
				t.Fatalf("mediaType = %q, want %q", gotType, tt.wantType)
			}
			if gotURL != mediaURL {
				t.Fatalf("url = %q, want %q", gotURL, mediaURL)
			}
			if gotDirectPath != directPath {
				t.Fatalf("directPath = %q, want %q", gotDirectPath, directPath)
			}
			if !bytes.Equal(gotMediaKey, mediaKey) {
				t.Fatalf("mediaKey = %q, want %q", gotMediaKey, mediaKey)
			}
			if !bytes.Equal(gotFileSHA256, fileSHA256) {
				t.Fatalf("fileSHA256 = %q, want %q", gotFileSHA256, fileSHA256)
			}
			if !bytes.Equal(gotFileEncSHA256, fileEncSHA256) {
				t.Fatalf("fileEncSHA256 = %q, want %q", gotFileEncSHA256, fileEncSHA256)
			}
			if gotFileLength != fileLength {
				t.Fatalf("fileLength = %d, want %d", gotFileLength, fileLength)
			}
		})
	}
}

func TestBuildDownloadableMessageSetsDirectPath(t *testing.T) {
	mediaKey := []byte("media-key")
	fileSHA256 := []byte("file-sha")
	fileEncSHA256 := []byte("file-enc-sha")
	fileLength := uint64(1234)
	directPath := "/v/t62.7118-24/media.enc?ccb=11-4"

	tests := []struct {
		name      string
		mediaType string
		wantType  any
	}{
		{name: "Image", mediaType: "image", wantType: &waE2E.ImageMessage{}},
		{name: "Video", mediaType: "video", wantType: &waE2E.VideoMessage{}},
		{name: "VideoNote", mediaType: "video_note", wantType: &waE2E.VideoMessage{}},
		{name: "Audio", mediaType: "audio", wantType: &waE2E.AudioMessage{}},
		{name: "PTT", mediaType: "ptt", wantType: &waE2E.AudioMessage{}},
		{name: "Document", mediaType: "document", wantType: &waE2E.DocumentMessage{}},
		{name: "Sticker", mediaType: "sticker", wantType: &waE2E.StickerMessage{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildDownloadableMessage(tt.mediaType, "https://mmg.whatsapp.net/ignored", directPath, "report.pdf", mediaKey, fileSHA256, fileEncSHA256, fileLength)
			if err != nil {
				t.Fatalf("BuildDownloadableMessage() error = %v", err)
			}
			if got.GetDirectPath() != directPath {
				t.Fatalf("directPath = %q, want %q", got.GetDirectPath(), directPath)
			}
			switch tt.wantType.(type) {
			case *waE2E.ImageMessage:
				if _, ok := got.(*waE2E.ImageMessage); !ok {
					t.Fatalf("message type = %T, want *waE2E.ImageMessage", got)
				}
			case *waE2E.VideoMessage:
				if _, ok := got.(*waE2E.VideoMessage); !ok {
					t.Fatalf("message type = %T, want *waE2E.VideoMessage", got)
				}
			case *waE2E.AudioMessage:
				if _, ok := got.(*waE2E.AudioMessage); !ok {
					t.Fatalf("message type = %T, want *waE2E.AudioMessage", got)
				}
			case *waE2E.DocumentMessage:
				if _, ok := got.(*waE2E.DocumentMessage); !ok {
					t.Fatalf("message type = %T, want *waE2E.DocumentMessage", got)
				}
			case *waE2E.StickerMessage:
				if _, ok := got.(*waE2E.StickerMessage); !ok {
					t.Fatalf("message type = %T, want *waE2E.StickerMessage", got)
				}
			}
		})
	}
}

func TestResolveMediaDirectPathFallsBackToURLRequestURI(t *testing.T) {
	storedDirectPath := "/stored/path.enc?auth=stored"
	mediaURL := "https://mmg.whatsapp.net/v/t62.7118-24/media.enc?ccb=11-4&oh=token"
	wantFallback := "/v/t62.7118-24/media.enc?ccb=11-4&oh=token"

	if got := ResolveMediaDirectPath(storedDirectPath, mediaURL); got != storedDirectPath {
		t.Fatalf("ResolveMediaDirectPath() = %q, want stored direct path %q", got, storedDirectPath)
	}
	if got := ResolveMediaDirectPath("", mediaURL); got != wantFallback {
		t.Fatalf("ResolveMediaDirectPath() = %q, want URL request URI %q", got, wantFallback)
	}
	if got := ResolveMediaDirectPath("", "://not-a-url"); got != "" {
		t.Fatalf("ResolveMediaDirectPath() = %q, want empty for invalid URL", got)
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
			locName: "Monas",
			address: "Gambir, Jakarta",
			lat:     -6.175392,
			long:    106.827153,
			want:    "Monas — Gambir, Jakarta — https://maps.google.com/?q=-6.175392,106.827153",
		},
		{
			name:    "NameOnly",
			locName: "Monas",
			address: "",
			lat:     -6.2,
			long:    106.8,
			want:    "Monas — https://maps.google.com/?q=-6.2,106.8",
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
			name:    "WhitespaceOnlyNameAndAddress",
			locName: " ",
			address: " ",
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

func TestExtractMessageTextFromProtoLocationMessages(t *testing.T) {
	tests := []struct {
		name string
		msg  *waE2E.Message
		want string
	}{
		{
			name: "LocationWithNameAndAddress",
			msg: &waE2E.Message{
				LocationMessage: &waE2E.LocationMessage{
					Name:             strPtr("Monas"),
					Address:          strPtr("Gambir, Jakarta"),
					DegreesLatitude:  proto.Float64(-6.2),
					DegreesLongitude: proto.Float64(106.8),
				},
			},
			want: "Monas — Gambir, Jakarta — https://maps.google.com/?q=-6.2,106.8",
		},
		{
			name: "LocationCoordinatesOnly",
			msg: &waE2E.Message{
				LocationMessage: &waE2E.LocationMessage{
					DegreesLatitude:  proto.Float64(-6.2),
					DegreesLongitude: proto.Float64(106.8),
				},
			},
			want: "https://maps.google.com/?q=-6.2,106.8",
		},
		{
			name: "LiveLocationWithCaption",
			msg: &waE2E.Message{
				LiveLocationMessage: &waE2E.LiveLocationMessage{
					Caption:          strPtr("On my way"),
					DegreesLatitude:  proto.Float64(-6.2),
					DegreesLongitude: proto.Float64(106.8),
				},
			},
			want: "On my way — https://maps.google.com/?q=-6.2,106.8",
		},
		{
			name: "LiveLocationWithoutCaption",
			msg: &waE2E.Message{
				LiveLocationMessage: &waE2E.LiveLocationMessage{
					DegreesLatitude:  proto.Float64(-6.2),
					DegreesLongitude: proto.Float64(106.8),
				},
			},
			want: "https://maps.google.com/?q=-6.2,106.8",
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

func TestExtractMessageTextFromProtoExtendedTextMessage(t *testing.T) {
	tests := []struct {
		name string
		msg  *waE2E.Message
		want string
	}{
		{
			name: "TextTakesPriorityOverMatchedText",
			msg: &waE2E.Message{
				ExtendedTextMessage: &waE2E.ExtendedTextMessage{
					Text:        strPtr("check this out https://example.com"),
					MatchedText: strPtr("https://example.com"),
				},
			},
			want: "check this out https://example.com",
		},
		{
			name: "FallsBackToMatchedTextWhenTextEmpty",
			msg: &waE2E.Message{
				ExtendedTextMessage: &waE2E.ExtendedTextMessage{
					Text:        strPtr(""),
					MatchedText: strPtr("https://example.com"),
				},
			},
			want: "https://example.com",
		},
		{
			name: "EmptyTextAndMatchedTextFallsThrough",
			msg: &waE2E.Message{
				ExtendedTextMessage: &waE2E.ExtendedTextMessage{
					Text:        strPtr(""),
					MatchedText: strPtr(""),
				},
			},
			want: "",
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

func TestBuildForwardMessageFromStorageText(t *testing.T) {
	msg, err := BuildForwardMessageFromStorage(&domainChatStorage.Message{
		Content: "hello forward",
	}, ForwardBuildOptions{})
	if err != nil {
		t.Fatalf("BuildForwardMessageFromStorage() error = %v", err)
	}
	ext := msg.GetExtendedTextMessage()
	if ext == nil {
		t.Fatal("expected ExtendedTextMessage")
	}
	if ext.GetText() != "hello forward" {
		t.Fatalf("text = %q, want %q", ext.GetText(), "hello forward")
	}
	if !ext.GetContextInfo().GetIsForwarded() {
		t.Fatal("expected IsForwarded=true")
	}
	if ext.GetContextInfo().GetForwardingScore() != 100 {
		t.Fatalf("forwarding score = %d, want 100", ext.GetContextInfo().GetForwardingScore())
	}
}

func TestBuildForwardMessageFromStorageImage(t *testing.T) {
	directPath := "/v/t62.media.enc"
	msg, err := BuildForwardMessageFromStorage(&domainChatStorage.Message{
		MediaType:     "image",
		Content:       "caption text",
		URL:           "https://mmg.whatsapp.net/ignored",
		DirectPath:    directPath,
		MediaKey:      []byte("key"),
		FileSHA256:    []byte("sha"),
		FileEncSHA256: []byte("enc"),
		FileLength:    42,
	}, ForwardBuildOptions{})
	if err != nil {
		t.Fatalf("BuildForwardMessageFromStorage() error = %v", err)
	}
	img := msg.GetImageMessage()
	if img == nil {
		t.Fatal("expected ImageMessage")
	}
	if img.GetCaption() != "caption text" {
		t.Fatalf("caption = %q, want %q", img.GetCaption(), "caption text")
	}
	if img.GetDirectPath() != directPath {
		t.Fatalf("directPath = %q, want %q", img.GetDirectPath(), directPath)
	}
	if !img.GetContextInfo().GetIsForwarded() {
		t.Fatal("expected IsForwarded=true")
	}
	if img.GetMimetype() != "image/jpeg" {
		t.Fatalf("mimetype = %q, want image/jpeg", img.GetMimetype())
	}
}

func TestBuildForwardMessageFromStorageDocument(t *testing.T) {
	msg, err := BuildForwardMessageFromStorage(&domainChatStorage.Message{
		MediaType:     "document",
		Filename:      "report.pdf",
		Content:       "see attached",
		URL:           "https://mmg.whatsapp.net/doc",
		DirectPath:    "/v/doc.enc",
		MediaKey:      []byte("key"),
		FileSHA256:    []byte("sha"),
		FileEncSHA256: []byte("enc"),
		FileLength:    100,
	}, ForwardBuildOptions{})
	if err != nil {
		t.Fatalf("BuildForwardMessageFromStorage() error = %v", err)
	}
	doc := msg.GetDocumentMessage()
	if doc == nil {
		t.Fatal("expected DocumentMessage")
	}
	if doc.GetFileName() != "report.pdf" {
		t.Fatalf("filename = %q, want report.pdf", doc.GetFileName())
	}
	if doc.GetCaption() != "see attached" {
		t.Fatalf("caption = %q, want see attached", doc.GetCaption())
	}
	if doc.GetMimetype() != "application/pdf" {
		t.Fatalf("mimetype = %q, want application/pdf", doc.GetMimetype())
	}
}

func TestBuildForwardMessageFromStorageUnsupportedType(t *testing.T) {
	_, err := BuildForwardMessageFromStorage(&domainChatStorage.Message{
		MediaType: "call",
		Content:   "incoming call",
	}, ForwardBuildOptions{})
	if err == nil {
		t.Fatal("expected error for call type")
	}
	if err.Error() != ErrUnsupportedForwardType {
		t.Fatalf("error = %q, want %q", err.Error(), ErrUnsupportedForwardType)
	}

	_, err = BuildForwardMessageFromStorage(&domainChatStorage.Message{
		Content: "Contact: Alice (+62 812)",
	}, ForwardBuildOptions{})
	if err == nil {
		t.Fatal("expected error for contact summary")
	}
}

func TestIsForwardableStorageMessage(t *testing.T) {
	tests := []struct {
		name    string
		message *domainChatStorage.Message
		want    bool
	}{
		{name: "text", message: &domainChatStorage.Message{Content: "hi"}, want: true},
		{name: "image", message: &domainChatStorage.Message{MediaType: "image", URL: "u", DirectPath: "/p"}, want: true},
		{name: "call", message: &domainChatStorage.Message{MediaType: "call"}, want: false},
		{name: "contact", message: &domainChatStorage.Message{Content: "Contact: Bob"}, want: false},
		{name: "location", message: &domainChatStorage.Message{Content: "Pin — https://maps.google.com/?q=1,2"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsForwardableStorageMessage(tt.message); got != tt.want {
				t.Fatalf("IsForwardableStorageMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}
