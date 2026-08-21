package chatwoot

import "testing"

// Covers escaping a segment in Chatwoot's public API path.
// The relevant case is a WhatsApp JID: url.PathEscape leaves "@" and "."
// untouched, while the Rails router cannot resolve the contact and returns HTML 404.
func TestEscapeChatwootPathSegment(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{
			name: "WhatsApp JID: escapes at sign and all dots",
			in:   "5511999999999@s.whatsapp.net",
			want: "5511999999999%40s%2Ewhatsapp%2Enet",
		},
		{
			name: "alphanumeric identifier remains unchanged",
			in:   "swkuTjDfw6W8jDcqTyriMNrd",
			want: "swkuTjDfw6W8jDcqTyriMNrd",
		},
		{
			name: "UUID source ID keeps hyphens",
			in:   "03205d06-0b89-4ff3-9e44-3d76a625c1da",
			want: "03205d06-0b89-4ff3-9e44-3d76a625c1da",
		},
		{
			name: "literal percent sign is not double encoded",
			in:   "a%b.c",
			want: "a%25b%2Ec",
		},
		{
			name: "space remains percent encoded instead of plus",
			in:   "a b.c",
			want: "a%20b%2Ec",
		},
		{
			name: "empty",
			in:   "",
			want: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := escapeChatwootPathSegment(tc.in); got != tc.want {
				t.Errorf("escapeChatwootPathSegment(%q)\n  got:  %q\n want: %q", tc.in, got, tc.want)
			}
		})
	}
}
