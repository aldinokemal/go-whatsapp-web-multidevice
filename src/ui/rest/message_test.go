package rest

import "testing"

func TestPublicStaticPath(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		want     string
	}{
		{
			name:     "media file under statics",
			filePath: "statics/media/628123/2026-06-09/audio file.ogg",
			want:     "/statics/media/628123/2026-06-09/audio%20file.ogg",
		},
		{
			name:     "windows separators",
			filePath: "statics\\media\\628123\\2026-06-09\\voice.ogg",
			want:     "/statics/media/628123/2026-06-09/voice.ogg",
		},
		{
			name:     "outside statics",
			filePath: "storages/audio.ogg",
			want:     "",
		},
		{
			name:     "path traversal",
			filePath: "../../etc/passwd",
			want:     "",
		},
		{
			name:     "empty path",
			filePath: "",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := publicStaticPath(tt.filePath); got != tt.want {
				t.Fatalf("publicStaticPath() = %q, want %q", got, tt.want)
			}
		})
	}
}
