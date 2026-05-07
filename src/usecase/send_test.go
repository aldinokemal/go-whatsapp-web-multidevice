package usecase

import (
	"errors"
	"testing"
)

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

func TestMaxBytesBufferAllowsWritesWithinLimit(t *testing.T) {
	buffer := &maxBytesBuffer{maxSize: 5}

	n, err := buffer.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	if n != 5 {
		t.Fatalf("Write() n = %d, want 5", n)
	}
	if got := buffer.String(); got != "hello" {
		t.Fatalf("buffer contents = %q, want %q", got, "hello")
	}
}

func TestMaxBytesBufferRejectsWritesOverLimit(t *testing.T) {
	buffer := &maxBytesBuffer{maxSize: 5}

	n, err := buffer.Write([]byte("hello!"))
	if !errors.Is(err, errImageExceedsMaxSize) {
		t.Fatalf("Write() error = %v, want %v", err, errImageExceedsMaxSize)
	}
	if n != 5 {
		t.Fatalf("Write() n = %d, want 5", n)
	}
	if got := buffer.String(); got != "hello" {
		t.Fatalf("buffer contents = %q, want %q", got, "hello")
	}
}
