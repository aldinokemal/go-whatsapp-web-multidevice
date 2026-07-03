package utils

import (
	"strings"
	"testing"
)

func TestMatchesIgnoredJID(t *testing.T) {
	tests := []struct {
		name   string
		jid    string
		ignore []string
		want   bool
	}{
		{
			// Empty jid is never ignored, even when patterns exist — guards the
			// early return that protects the live-forward path from blank JIDs.
			name:   "EmptyJIDWithNonEmptyIgnore",
			jid:    "",
			ignore: []string{"@g.us"},
			want:   false,
		},
		{
			// No patterns means nothing to match against.
			name:   "EmptyIgnoreList",
			jid:    "123@s.whatsapp.net",
			ignore: nil,
			want:   false,
		},
		{
			// Blank/whitespace patterns are skipped, so an otherwise-empty list
			// cannot accidentally match every JID.
			name:   "WhitespaceOnlyPatternsSkipped",
			jid:    "123@s.whatsapp.net",
			ignore: []string{"", "   ", "\t"},
			want:   false,
		},
		{
			// Suffix wildcard for the group space matches any group JID.
			name:   "GroupWildcardMatchesGroup",
			jid:    "120363012345678901@g.us",
			ignore: []string{"@g.us"},
			want:   true,
		},
		{
			// The group wildcard must NOT match a DM in a different address space.
			name:   "GroupWildcardDoesNotMatchDM",
			jid:    "123@s.whatsapp.net",
			ignore: []string{"@g.us"},
			want:   false,
		},
		{
			// DM wildcard matches any direct-message JID.
			name:   "DMWildcardMatchesDM",
			jid:    "6281234567890@s.whatsapp.net",
			ignore: []string{"@s.whatsapp.net"},
			want:   true,
		},
		{
			// DM wildcard must NOT match a group JID.
			name:   "DMWildcardDoesNotMatchGroup",
			jid:    "120363012345678901@g.us",
			ignore: []string{"@s.whatsapp.net"},
			want:   false,
		},
		{
			// lid wildcard matches lid-space JIDs.
			name:   "LidWildcardMatchesLid",
			jid:    "12345@lid",
			ignore: []string{"@lid"},
			want:   true,
		},
		{
			// lid wildcard must NOT bleed into the DM space.
			name:   "LidWildcardDoesNotMatchDM",
			jid:    "12345@s.whatsapp.net",
			ignore: []string{"@lid"},
			want:   false,
		},
		{
			// An exact JID pattern matches that exact JID.
			name:   "ExactJIDMatch",
			jid:    "123@s.whatsapp.net",
			ignore: []string{"123@s.whatsapp.net"},
			want:   true,
		},
		{
			// Critical: an exact (non-wildcard) pattern must NOT match by suffix.
			// "123@s.whatsapp.net" must not ignore "999123@s.whatsapp.net",
			// otherwise one blocked number would shadow many unrelated numbers.
			name:   "ExactPatternDoesNotMatchBySuffix",
			jid:    "999123@s.whatsapp.net",
			ignore: []string{"123@s.whatsapp.net"},
			want:   false,
		},
		{
			// Exact pattern that differs from the JID does not match.
			name:   "ExactPatternMismatch",
			jid:    "123@s.whatsapp.net",
			ignore: []string{"456@s.whatsapp.net"},
			want:   false,
		},
		{
			// Surrounding spaces on a wildcard pattern are trimmed before
			// matching — config values are often whitespace-padded.
			name:   "WildcardPatternTrimmed",
			jid:    "120363012345678901@g.us",
			ignore: []string{"  @g.us  "},
			want:   true,
		},
		{
			// Surrounding spaces on an exact pattern are trimmed too.
			name:   "ExactPatternTrimmed",
			jid:    "123@s.whatsapp.net",
			ignore: []string{"  123@s.whatsapp.net  "},
			want:   true,
		},
		{
			// First pattern misses, a later pattern matches — the loop must scan
			// the whole list, not stop at the first non-match.
			name:   "LaterPatternMatches",
			jid:    "120363012345678901@g.us",
			ignore: []string{"123@s.whatsapp.net", "", "@g.us"},
			want:   true,
		},
		{
			// Multiple patterns, none match.
			name:   "MultiplePatternsNoMatch",
			jid:    "12345@lid",
			ignore: []string{"@g.us", "@s.whatsapp.net", "999@lid"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchesIgnoredJID(tt.jid, tt.ignore); got != tt.want {
				t.Fatalf("MatchesIgnoredJID(%q, %v) = %v, want %v", tt.jid, tt.ignore, got, tt.want)
			}
		})
	}
}

func TestIsNewsletterJID(t *testing.T) {
	// Newsletter (channel) JIDs like 120363144038483540@newsletter are
	// broadcast feeds, not conversations. Their local part is an 18-digit
	// channel id — not a phone number — so letting one reach the Chatwoot
	// contact-creation phone path always fails with a 422 "Phone number
	// should be in e164 format" (E.164 caps at 15 digits).
	tests := []struct {
		name string
		jid  string
		want bool
	}{
		{name: "newsletter JID", jid: "120363144038483540@newsletter", want: true},
		{name: "ordinary user JID", jid: "628123456789@s.whatsapp.net", want: false},
		{name: "group JID", jid: "120363123@g.us", want: false},
		{name: "lid JID", jid: "abc123@lid", want: false},
		{name: "empty string", jid: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNewsletterJID(tt.jid); got != tt.want {
				t.Fatalf("IsNewsletterJID(%q) = %v, want %v", tt.jid, got, tt.want)
			}
		})
	}
}

func TestIsSystemBroadcastJID(t *testing.T) {
	// Pinning the exact strings prevents a future "match by suffix" rewrite
	// from letting real-status-bearing JIDs (e.g. "status@s.whatsapp.net")
	// through, or blocking legitimate user JIDs that happen to start with "0".
	// Single source of truth for both the live-forward and history-import paths.
	tests := []struct {
		name string
		jid  string
		want bool
	}{
		{name: "status broadcast", jid: "status@broadcast", want: true},
		{name: "wa service account", jid: "0@s.whatsapp.net", want: true},
		{name: "ordinary user JID", jid: "628123456789@s.whatsapp.net", want: false},
		{name: "group JID", jid: "120363123@g.us", want: false},
		{name: "lid JID", jid: "abc123@lid", want: false},
		{name: "user starting with 0 is NOT system", jid: "012345@s.whatsapp.net", want: false},
		{name: "empty string", jid: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSystemBroadcastJID(tt.jid); got != tt.want {
				t.Fatalf("IsSystemBroadcastJID(%q) = %v, want %v", tt.jid, got, tt.want)
			}
		})
	}
}

func TestChatwootToWhatsAppMarkdown(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			// Empty string short-circuits the fast path.
			name: "Empty",
			in:   "",
			want: "",
		},
		{
			// Plain text with no * or ~ takes the ContainsAny fast path and is
			// returned byte-for-byte unchanged.
			name: "PlainTextNoMarkersFastPath",
			in:   "hello world",
			want: "hello world",
		},
		{
			// Underscores alone are not a Chatwoot marker here, so the fast path
			// returns them untouched (no * or ~ present).
			name: "UnderscoresUntouchedFastPath",
			in:   "a_b_c",
			want: "a_b_c",
		},
		{
			// **bold** -> *bold*.
			name: "BoldAlone",
			in:   "**bold**",
			want: "*bold*",
		},
		{
			// Guard the documented invariant: bold must become a single
			// asterisk, never get re-italicized into _x_.
			name: "BoldNotReItalicized",
			in:   "**x**",
			want: "*x*",
		},
		{
			// *italic* -> _italic_. The braced "${1}" template terminates the
			// group reference before the trailing underscore, so the capture is
			// preserved (a bare "$1_" would parse "1_" as a named group and drop it).
			name: "ItalicAlone",
			in:   "*italic*",
			want: "_italic_",
		},
		{
			// ~~strike~~ -> ~strike~. The strike template "~$1~" is safe because
			// "~" is not a valid $name character, so $1 expands normally.
			name: "StrikeAlone",
			in:   "~~strike~~",
			want: "~strike~",
		},
		{
			// Combined input: bold, italic, and strike all convert independently.
			name: "Combined",
			in:   "**b** and *i* and ~~s~~",
			want: "*b* and _i_ and ~s~",
		},
		{
			// A lone unmatched asterisk has no closing pair, so it is left alone.
			name: "LoneAsteriskUnchanged",
			in:   "2 * 3",
			want: "2 * 3",
		},
		{
			// Single closing-less asterisk in a sentence stays put.
			name: "SingleTrailingAsterisk",
			in:   "value*",
			want: "value*",
		},
		{
			// A lone tilde without a pair is left unchanged.
			name: "LoneTildeUnchanged",
			in:   "approx ~ 5",
			want: "approx ~ 5",
		},
		{
			// A pre-existing sentinel control rune in the input must be stripped
			// up front, never restored into stray markdown — defends the bold/
			// strike placeholder mechanism against crafted input.
			name: "PreExistingSentinelStripped",
			in:   "\x01**bold**\x02",
			want: "*bold*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ChatwootToWhatsAppMarkdown(tt.in)
			if got != tt.want {
				t.Fatalf("ChatwootToWhatsAppMarkdown(%q) = %q, want %q", tt.in, got, tt.want)
			}
			// No sentinel runes may ever leak into output.
			assertNoMarkdownSentinels(t, got)
		})
	}
}

func TestWhatsAppToChatwootMarkdown(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			// Empty string short-circuits the fast path.
			name: "Empty",
			in:   "",
			want: "",
		},
		{
			// Plain text without *, _ or ~ is returned unchanged via fast path.
			name: "PlainTextNoMarkersFastPath",
			in:   "hello world",
			want: "hello world",
		},
		{
			// *bold* -> **bold**.
			name: "BoldAlone",
			in:   "*bold*",
			want: "**bold**",
		},
		{
			// _italic_ -> *italic*.
			name: "ItalicAlone",
			in:   "_italic_",
			want: "*italic*",
		},
		{
			// ~strike~ -> ~~strike~~.
			name: "StrikeAlone",
			in:   "~strike~",
			want: "~~strike~~",
		},
		{
			// All three convert independently; the freshly-created italic
			// asterisks must not be re-read as bold.
			name: "Combined",
			in:   "*b* and _i_ and ~s~",
			want: "**b** and *i* and ~~s~~",
		},
		{
			// A lone unmatched asterisk is left alone (no closing pair).
			name: "LoneAsteriskUnchanged",
			in:   "2 * 3",
			want: "2 * 3",
		},
		{
			// A lone underscore without a pair is left unchanged.
			name: "LoneUnderscoreUnchanged",
			in:   "a_b",
			want: "a_b",
		},
		{
			// A lone tilde without a pair is left unchanged.
			name: "LoneTildeUnchanged",
			in:   "approx ~ 5",
			want: "approx ~ 5",
		},
		{
			// A pre-existing sentinel control rune in the input must be stripped
			// up front, never restored into stray markdown.
			name: "PreExistingSentinelStripped",
			in:   "\x01*bold*\x02",
			want: "**bold**",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WhatsAppToChatwootMarkdown(tt.in)
			if got != tt.want {
				t.Fatalf("WhatsAppToChatwootMarkdown(%q) = %q, want %q", tt.in, got, tt.want)
			}
			// The defensive sentinel-stripping pass must keep \x01/\x02 out of
			// every output, even for inputs that never created a sentinel.
			assertNoMarkdownSentinels(t, got)
		})
	}
}

// TestChatwootMarkdownRoundTrip is a sanity check that simple WhatsApp-flavored
// strings survive a WhatsApp -> Chatwoot -> WhatsApp round trip intact.
func TestChatwootMarkdownRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		wa   string
	}{
		{name: "Bold", wa: "*bold*"},
		{name: "Italic", wa: "_italic_"},
		{name: "Strike", wa: "~strike~"},
		{name: "Combined", wa: "*b* and _i_ and ~s~"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cw := WhatsAppToChatwootMarkdown(tt.wa)
			back := ChatwootToWhatsAppMarkdown(cw)
			if back != tt.wa {
				t.Fatalf("round trip %q -> %q -> %q, want original", tt.wa, cw, back)
			}
		})
	}
}

// assertNoMarkdownSentinels fails if the markdown bridge leaked either of the
// internal guard runes (\x01 bold, \x02 strike) into a user-visible string.
func assertNoMarkdownSentinels(t *testing.T, out string) {
	t.Helper()
	if strings.ContainsAny(out, "\x01\x02") {
		t.Fatalf("output leaked a markdown sentinel rune: %q", out)
	}
}
