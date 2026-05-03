package usecase

import "testing"

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
