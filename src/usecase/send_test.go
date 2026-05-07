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

func TestPDFHasSafeFirstPageGeometry(t *testing.T) {
	tests := []struct {
		name string
		pdf  []byte
		want bool
	}{
		{
			name: "normal media box",
			pdf:  []byte("%PDF-1.7\n1 0 obj<</MediaBox [0 0 612 792]>>endobj"),
			want: true,
		},
		{
			name: "huge media box",
			pdf:  []byte("%PDF-1.7\n1 0 obj<</MediaBox [0 0 144000 144000]>>endobj"),
			want: false,
		},
		{
			name: "invalid dimensions",
			pdf:  []byte("%PDF-1.7\n1 0 obj<</MediaBox [0 0 0 792]>>endobj"),
			want: false,
		},
		{
			name: "no media box",
			pdf:  []byte("%PDF-1.7\n"),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pdfHasSafeFirstPageGeometry(tt.pdf); got != tt.want {
				t.Fatalf("pdfHasSafeFirstPageGeometry() = %t, want %t", got, tt.want)
			}
		})
	}
}
