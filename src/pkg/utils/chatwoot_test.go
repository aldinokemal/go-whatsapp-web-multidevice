package utils

import (
	"strings"
	"testing"
	"time"
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
			// Plain text with nothing that could be Markdown skips the
			// conversion and is returned byte-for-byte unchanged.
			name: "PlainTextNoMarkersFastPath",
			in:   "hello world",
			want: "hello world",
		},
		{
			// Underscores alone are not a Chatwoot marker here, so the fast path
			// returns them untouched.
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
			// *italic* -> _italic_. The replacement template braces its group
			// reference: a bare "$2_" would be read as the group named "2_" and
			// drop the text.
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
		{
			name: "MixedEmphasisInSentence",
			in:   "Dear **Dana**, your *cabin* is ~~12A~~ **14B**.",
			want: "Dear *Dana*, your _cabin_ is ~12A~ *14B*.",
		},
		{
			// ***x*** used to come out as "_x*_".
			name: "BoldItalic",
			in:   "***Cancun, Mexico***\n**Total**",
			want: "*_Cancun, Mexico_*\n*Total*",
		},
		{
			name: "MultiLine",
			in:   "line one\n\n**line two**\nline *three*",
			want: "line one\n\n*line two*\nline _three_",
		},
		{
			name: "EmojiAndBold",
			in:   "🌴 **Welcome** 🚢 see you ~~soon~~ *now* 👍🏽",
			want: "🌴 *Welcome* 🚢 see you ~soon~ _now_ 👍🏽",
		},
		{
			name: "RightToLeftText",
			in:   "**שלום** לכולם, *תודה* רבה: [האתר שלנו](https://x.example/he)",
			want: "*שלום* לכולם, _תודה_ רבה: האתר שלנו: https://x.example/he",
		},
		{
			// Bullets and numbering are plain text on WhatsApp; Chatwoot
			// escapes the dot of a typed "1." to keep it from becoming a list.
			name: "ListsPreserved",
			in:   "Options:\n- **Balcony** cabin\n- Inside cabin\n\n1. Pay deposit\n2. Send passports\n3\\. Done",
			want: "Options:\n- *Balcony* cabin\n- Inside cabin\n\n1. Pay deposit\n2. Send passports\n3. Done",
		},
		{
			name: "InlineLink",
			in:   "- [Symphony of the Seas](https://x.example/royal/symphony-of-the-seas/jan-24-2027/)  ",
			want: "- Symphony of the Seas: https://x.example/royal/symphony-of-the-seas/jan-24-2027/  ",
		},
		{
			// A canned reply whose link label is the URL: one URL, not
			// "url: url" and not the raw brackets.
			name: "InlineLinkLabelIsBoldURL",
			in:   "form: [**https://x.example/events/registration/**](https://x.example/events/registration/)",
			want: "form: https://x.example/events/registration/",
		},
		{
			name: "InlineLinkLabelIsURLAfterEmoji",
			in:   "🌐 [**https://x.example/**](https://x.example/)",
			want: "🌐 https://x.example/",
		},
		{
			name: "InlineLinkLabelIsURLIgnoringTrailingSlashAndCase",
			in:   "[_HTTPS://X.example_](https://x.example/)",
			want: "https://x.example/",
		},
		{
			// The escaped "&" used to end the link on the phone.
			name: "InlineLinkEscapedAmpersandInTarget",
			in:   "[https://x.example/a?shareId=5&asset=1](https://x.example/a?shareId=5\\&asset=1)",
			want: "https://x.example/a?shareId=5&asset=1",
		},
		{
			name: "InlineLinkMailto",
			in:   "email: [**rabbi_usa@x.example**](mailto:rabbi_usa@x.example)",
			want: "email: rabbi_usa@x.example",
		},
		{
			name: "InlineLinkTel",
			in:   "[Call us](tel:+15550100)",
			want: "Call us: +15550100",
		},
		{
			// The label keeps its emphasis; the title is dropped.
			name: "InlineLinkBoldLabelAndTitle",
			in:   "see [**Splash Academy**](https://www.x.example/kids \"Kids\") now",
			want: "see *Splash Academy*: https://www.x.example/kids now",
		},
		{
			name: "InlineLinkBalancedParentheses",
			in:   "[wiki](https://x.example/wiki/Mercury_(planet)) ok",
			want: "wiki: https://x.example/wiki/Mercury_(planet) ok",
		},
		{
			name: "InlineLinkEmptyLabel",
			in:   "[](https://x.example/a) [  ](<https://x.example/b>)",
			want: "https://x.example/a https://x.example/b",
		},
		{
			// Emphasis around a whole link still converts; the target's own
			// delimiters do not take part.
			name: "InlineLinkInsideBoldWithDelimitersInTarget",
			in:   "**[Book *now*](https://x.example/a_b/*c*/?utm_source=cw&utm_medium=~x~)**",
			want: "*Book _now_: https://x.example/a_b/*c*/?utm_source=cw&utm_medium=~x~*",
		},
		{
			name: "TwoInlineLinksOnOneLine",
			in:   "[A](https://x.example/a) and [B](https://x.example/b)",
			want: "A: https://x.example/a and B: https://x.example/b",
		},
		{
			name: "MessageThatIsOnlyALink",
			in:   "https://x.example/t?utm_source=wa&utm_medium=tpl",
			want: "https://x.example/t?utm_source=wa&utm_medium=tpl",
		},
		{
			name: "MessageThatIsOnlyAMarkdownLink",
			in:   "[https://x.example/](https://x.example/)",
			want: "https://x.example/",
		},
		{
			name: "NotALink",
			in:   "2 < 3 and [not a link] (really), [relative](/path), a[0](x)",
			want: "2 < 3 and [not a link] (really), [relative](/path), a[0](x)",
		},
		{
			// An escaped bracket is literal text, not the start of a link.
			name: "EscapedBracketsAreNotALink",
			in:   "\\[label\\](https://x.example/)",
			want: "[label](https://x.example/)",
		},
		{
			name: "Autolink",
			in:   "here: <https://x.example/3qdiakdIIl0?si=4_xh80NjuF5HdWea>",
			want: "here: https://x.example/3qdiakdIIl0?si=4_xh80NjuF5HdWea",
		},
		{
			name: "AutolinkMailtoAndEmail",
			in:   "write <info@x.example> or <mailto:first\\_last@x.example>, <not an autolink>",
			want: "write info@x.example or first_last@x.example, <not an autolink>",
		},
		{
			name: "BackslashEscapes",
			in:   "a \\- b 1\\. c snake\\_case R\\&D \\(x\\) \\#1 \\+ \\! \\\\ \\[y\\]",
			want: "a - b 1. c snake_case R&D (x) #1 + ! \\ [y]",
		},
		{
			// A backslash before a letter or digit is not an escape.
			name: "BackslashBeforeAlphanumericKept",
			in:   "C:\\temp\\1 and a trailing \\",
			want: "C:\\temp\\1 and a trailing \\",
		},
		{
			// Escaped delimiters are literal: they must not pair up.
			name: "EscapedDelimitersDoNotFormat",
			in:   "5 \\* 3 \\* 2 and \\~x\\~ and **b\\*b**",
			want: "5 * 3 * 2 and ~x~ and *b*b*",
		},
		{
			name: "EscapedAmpersandInBareURL",
			in:   "https://x.example/q?a=1\\&b=2\\&c\\_d=3 ok",
			want: "https://x.example/q?a=1&b=2&c_d=3 ok",
		},
		{
			name: "BareURLQueryUnchanged",
			in:   "plain https://x.example/q?a=2&t=0&c=0&i=0&f=1&it=1",
			want: "plain https://x.example/q?a=2&t=0&c=0&i=0&f=1&it=1",
		},
		{
			// The two asterisks in the URL used to pair as italic.
			name: "BareURLWithAsterisksUnchanged",
			in:   "https://x.example/search?q=*summer*&sort=~price~ and www.x.example/*a*/~b~",
			want: "https://x.example/search?q=*summer*&sort=~price~ and www.x.example/*a*/~b~",
		},
		{
			name: "BareURLWithUnderscoresNextToItalic",
			in:   "*see* https://x.example/snake_case_path/_docs_/?utm_source=cw *now*",
			want: "_see_ https://x.example/snake_case_path/_docs_/?utm_source=cw _now_",
		},
		{
			name: "BoldBareURL",
			in:   "**https://x.example/manual-payment**",
			want: "*https://x.example/manual-payment*",
		},
		{
			name: "EmphasisSpanningBareURL",
			in:   "**pay at https://x.example/a*b*c today** and ~~https://x.example/~old/x~~",
			want: "*pay at https://x.example/a*b*c today* and ~https://x.example/~old/x~",
		},
		{
			// What Chatwoot's reply box stores when a canned reply holding
			// [**url**](url) is inserted.
			name: "DuplicatedLinkFromCannedReply",
			in:   "form: **https://x.example/events/registration/**: https://x.example/events/registration/\n\nTo help",
			want: "form: https://x.example/events/registration/\n\nTo help",
		},
		{
			name: "DuplicatedLinkWithoutEmphasis",
			in:   "https://x.example/a_b: https://x.example/a_b",
			want: "https://x.example/a_b",
		},
		{
			name: "DuplicatedLinkTwice",
			in:   "*https://x.example/a*: https://x.example/a and https://x.example/b: https://x.example/b",
			want: "https://x.example/a and https://x.example/b",
		},
		{
			name: "DifferentLinksAreNotDuplicates",
			in:   "https://x.example/x: https://x.example/xy, Site https://x.example/x: https://y.example/y, https://x.example/x - https://x.example/x",
			want: "https://x.example/x: https://x.example/xy, Site https://x.example/x: https://y.example/y, https://x.example/x - https://x.example/x",
		},
		{
			// The opening marker is missing, so this is not the reply box's
			// flattened link; keep both.
			name: "UnbalancedMarkerIsNotADuplicate",
			in:   "https://x.example/x**: https://x.example/x",
			want: "https://x.example/x**: https://x.example/x",
		},
		{
			name: "InlineCodeUntouched",
			in:   "run `**not bold** [a](https://x.example/) \\_ <https://x.example/>` then **bold**",
			want: "run `**not bold** [a](https://x.example/) \\_ <https://x.example/>` then *bold*",
		},
		{
			name: "CodeBlockUntouched",
			in:   "**before**\n```\n**a** *b* ~~c~~ \\* [l](https://x.example/)\n--\n```\n*after*",
			want: "*before*\n```\n**a** *b* ~~c~~ \\* [l](https://x.example/)\n--\n```\n_after_",
		},
		{
			// An escaped backtick is a literal backtick, not a code span.
			name: "EscapedBacktickIsNotCode",
			in:   "\\`**bold**\\`",
			want: "`*bold*`",
		},
		{
			// Signature switch on, signature empty: Chatwoot appends only the
			// separator.
			name: "EmptySignatureSeparatorDropped",
			in:   "Thanks **a lot**\n\n\n--\n\n",
			want: "Thanks *a lot*",
		},
		{
			name: "EmptySignatureSeparatorDroppedPlainText",
			in:   "Thanks\r\n\r\n  -- \t\r\n",
			want: "Thanks",
		},
		{
			name: "OnlySignatureSeparatorPadded",
			in:   "\n\n--\n\n",
			want: "",
		},
		{
			name: "OnlySignatureSeparator",
			in:   "--",
			want: "",
		},
		{
			name: "EscapedSignatureSeparator",
			in:   "ok\n\n\\-\\-",
			want: "ok",
		},
		{
			name: "SignatureWithTextKept",
			in:   "Thanks\n\n--\nDana\nAcme Travel",
			want: "Thanks\n\n--\nDana\nAcme Travel",
		},
		{
			name: "DashesInSentenceKept",
			in:   "prices -- as agreed\nsee you --",
			want: "prices -- as agreed\nsee you --",
		},
		{
			name: "LongerDashLineKept",
			in:   "above\n---",
			want: "above\n---",
		},
		{
			// Trailing whitespace is only trimmed together with a separator.
			name: "TrailingWhitespaceKept",
			in:   "hello  \n",
			want: "hello  \n",
		},
		{
			// Arithmetic is not emphasis: a delimiter has to hug the text.
			name: "SpacedAsterisksUnchanged",
			in:   "2 * 3 * 4 = 24 and 10 * 20",
			want: "2 * 3 * 4 = 24 and 10 * 20",
		},
		{
			// Dimensions: WhatsApp would show "30_40_50" literally, so a
			// mid-word asterisk pair is left as typed.
			name: "MidWordAsterisksUnchanged",
			in:   "Box 30*40*50 cm, un*believ*able",
			want: "Box 30*40*50 cm, un*believ*able",
		},
		{
			name: "FootnoteAsterisksUnchanged",
			in:   "Price* includes tax, rated 5* and 4*.\n*Subject to change",
			want: "Price* includes tax, rated 5* and 4*.\n*Subject to change",
		},
		{
			// A "*" bullet used to pair with the emphasis on its own line.
			name: "AsteriskBulletsWithEmphasis",
			in:   "* cabin with *balcony*\n* **inside** cabin\n* plain",
			want: "* cabin with _balcony_\n* *inside* cabin\n* plain",
		},
		{
			name: "MaskedDigitsUnchanged",
			in:   "Card ending ****1234, PIN ******, code ~~~~",
			want: "Card ending ****1234, PIN ******, code ~~~~",
		},
		{
			name: "SpacedPairsUnchanged",
			in:   "~~ a ~~ and ** b ** and * c * and *** d ***",
			want: "~~ a ~~ and ** b ** and * c * and *** d ***",
		},
		{
			// A closing run preceded by a space cannot close; the next one can.
			name: "CloserMustHugText",
			in:   "**a ** b** and *c * d* and ~~e ~~ f~~",
			want: "*a ** b* and _c * d_ and ~e ~~ f~",
		},
		{
			name: "AdjacentPairs",
			in:   "*a* *b* *c*, **d** **e**, ~~f~~ ~~g~~, *h*,*i*,*j*",
			want: "_a_ _b_ _c_, *d* *e*, ~f~ ~g~, _h_,_i_,_j_",
		},
		{
			name: "ItalicNextToPunctuation",
			in:   "(*a*) \"*b*\" *c*, *d*. *e*! *f*? [*g*] *h*:",
			want: "(_a_) \"_b_\" _c_, _d_. _e_! _f_? [_g_] _h_:",
		},
		{
			// Italic at either end of bold, as the reply editor writes it.
			name: "ItalicNestedInBold",
			in:   "***it* bold** and **bold *it*** and **a *b* c**",
			want: "*_it_ bold* and *bold _it_* and *a _b_ c*",
		},
		{
			name: "BoldNestedInItalic",
			in:   "*it **bold*** and *a **b** c*",
			want: "_it *bold*_ and _a *b* c_",
		},
		{
			name: "BoldThenItalicWithoutSpace",
			in:   "**a***b* and **a****b**",
			want: "*a*_b_ and *a**b*",
		},
		{
			name: "PricesTimesAndPhones",
			in:   "$1,200 - $1,500 per person (10:30 - 11:45), call +1 (555) 010-9999 or +972-50-123-4567 ~ 5 min",
			want: "$1,200 - $1,500 per person (10:30 - 11:45), call +1 (555) 010-9999 or +972-50-123-4567 ~ 5 min",
		},
		{
			name: "PriceInBold",
			in:   "Total: **$1,200 - $1,500**/person* (~10% off)",
			want: "Total: *$1,200 - $1,500*/person* (~10% off)",
		},
		{
			name: "EmailAddresses",
			in:   "write info@x.example, a_b@x.example or **sales@x.example**",
			want: "write info@x.example, a_b@x.example or *sales@x.example*",
		},
		{
			name: "FileNamesWithUnderscores",
			in:   "see report_final_v2.pdf, my\\_file\\_name.pdf and *price_list_2027.xlsx*",
			want: "see report_final_v2.pdf, my_file_name.pdf and _price_list_2027.xlsx_",
		},
		{
			name: "HebrewNextToURL",
			in:   "**ראו:** https://x.example/דף?a_b=1 *תודה* (<https://x.example/he_il>)",
			want: "*ראו:* https://x.example/דף?a_b=1 _תודה_ (https://x.example/he_il)",
		},
		{
			name: "ArabicNextToURL",
			in:   "مرحبا **بكم** في [موقعنا](https://x.example/ar_page) و https://x.example/a*b*.",
			want: "مرحبا *بكم* في موقعنا: https://x.example/ar_page و https://x.example/a*b*.",
		},
		{
			name: "SeveralLinksInOneMessage",
			in:   "[a](https://a.example/x_y) and [b](https://b.example/*z*), also https://c.example/~w~! <https://d.example/>",
			want: "a: https://a.example/x_y and b: https://b.example/*z*, also https://c.example/~w~! https://d.example/",
		},
		{
			name: "NumberedAndBulletedLists",
			in:   "1. **First**\n2. *Second*\n3. ~~Third~~\n- a\n+ b\n* c\n\n1) d\n10\\. e",
			want: "1. *First*\n2. _Second_\n3. ~Third~\n- a\n+ b\n* c\n\n1) d\n10. e",
		},
		{
			name: "URLInParentheses",
			in:   "our site (https://x.example/a_b) and (see *https://x.example/c_d*).",
			want: "our site (https://x.example/a_b) and (see _https://x.example/c_d_).",
		},
		{
			name: "URLFollowedByPeriod",
			in:   "Visit https://x.example/a_b. Or [here](https://x.example/c_d). Thanks",
			want: "Visit https://x.example/a_b. Or here: https://x.example/c_d. Thanks",
		},
		{
			name: "URLWithParentheses",
			in:   "https://x.example/wiki/Foo_(bar) and [w](https://x.example/wiki/Foo_(bar))",
			want: "https://x.example/wiki/Foo_(bar) and w: https://x.example/wiki/Foo_(bar)",
		},
		{
			name: "ImageLink",
			in:   "![logo](https://x.example/logo_small.png)",
			want: "logo: https://x.example/logo_small.png",
		},
		{
			name: "AutolinkTel",
			in:   "call <tel:+15550100>",
			want: "call +15550100",
		},
		{
			name: "AngleBracketsThatAreNotLinks",
			in:   "a < b and c > d, <3, <b>x</b>, <not a link>",
			want: "a < b and c > d, <3, <b>x</b>, <not a link>",
		},
		{
			name: "BracketsThatAreNotLinks",
			in:   "[x] (https://x.example/a) and [x](not a link) and [1] and [a\nb](https://x.example/b)",
			want: "[x] (https://x.example/a) and [x](not a link) and [1] and [a\nb](https://x.example/b)",
		},
		{
			name: "BackslashesInPaths",
			in:   "C:\\Users\\name and C:\\\\temp\\\\x, 50\\% off \\#1",
			want: "C:\\Users\\name and C:\\temp\\x, 50% off #1",
		},
		{
			// A link sentinel in the input must not be read as a placeholder.
			name: "PreExistingLinkSentinelStripped",
			in:   "\x030\x03 **a** \x03 https://x.example/",
			want: "0 *a*  https://x.example/",
		},
		{
			// A label that differs from the target only by a literal "_" and
			// by path case names another address; it is kept.
			name: "LinkLabelDifferingByUnderscoreAndPathCaseKept",
			in:   "[https://x.example/Invoice_A](https://x.example/invoicea)",
			want: "https://x.example/Invoice_A: https://x.example/invoicea",
		},
		{
			// Paths are case-sensitive.
			name: "LinkLabelDifferingByPathCaseKept",
			in:   "[https://x.example/Invoice](https://x.example/invoice)",
			want: "https://x.example/Invoice: https://x.example/invoice",
		},
		{
			// So are queries and fragments.
			name: "LinkLabelDifferingByQueryOrFragmentCaseKept",
			in:   "[https://x.example/a?Id=1](https://x.example/a?id=1) [https://x.example/a#Top](https://x.example/a#top)",
			want: "https://x.example/a?Id=1: https://x.example/a?id=1 https://x.example/a#Top: https://x.example/a#top",
		},
		{
			// "_", "*" and "~" inside an address are part of it.
			name: "LinkLabelDifferingByLiteralFormattingCharactersKept",
			in:   "[https://x.example/a_b](https://x.example/ab) [https://x.example/a\\*b](https://x.example/ab) [https://x.example/~u](https://x.example/u)",
			want: "https://x.example/a_b: https://x.example/ab https://x.example/a*b: https://x.example/ab https://x.example/~u: https://x.example/u",
		},
		{
			// A trailing "_" that has no opening partner is not formatting.
			name: "LinkLabelDifferingByTrailingUnderscoreKept",
			in:   "[https://x.example/a_](https://x.example/a)",
			want: "https://x.example/a_: https://x.example/a",
		},
		{
			// Only a slash after the host is ignored, not one that ends a path.
			name: "LinkLabelDifferingByPathTrailingSlashKept",
			in:   "[https://x.example/a](https://x.example/a/)",
			want: "https://x.example/a: https://x.example/a/",
		},
		{
			// Scheme and host are case-insensitive, and the formatting around
			// the label is not part of it: same address, emitted once.
			name: "LinkLabelSameAddressDifferentHostCaseCollapsed",
			in:   "[**HTTPS://X.Example/Invoice_A**](https://x.example/Invoice_A) [*~~https://X.EXAMPLE~~*](https://x.example/)",
			want: "https://x.example/Invoice_A https://x.example/",
		},
		{
			// The label may repeat the address with its "_" escaped or as code.
			name: "LinkLabelEscapedOrCodeSameAddressCollapsed",
			in:   "[https://x.example/a\\_b](https://x.example/a_b) [`https://x.example/c_d`](https://x.example/c_d)",
			want: "https://x.example/a_b https://x.example/c_d",
		},
		{
			// The closing markers end the link even when text follows them
			// without a space.
			name: "BoldBareURLFollowedByPunctuationAndText",
			in:   "**https://x.example**!Thanks",
			want: "*https://x.example*!Thanks",
		},
		{
			name: "EmphasisedBareURLsFollowedByPunctuationAndText",
			in:   "*https://x.example/a_b*,ok ~~https://x.example/c~~.gone ***https://x.example/d***;e",
			want: "_https://x.example/a_b_,ok ~https://x.example/c~.gone *_https://x.example/d_*;e",
		},
		{
			// The emphasis may open before the link.
			name: "BoldSentenceEndingInURLFollowedByPunctuationAndText",
			in:   "**see https://x.example/a**!Thanks",
			want: "*see https://x.example/a*!Thanks",
		},
		{
			name: "AdjacentBoldURLsWithoutSpaces",
			in:   "**https://a.example**,**https://b.example**!x",
			want: "*https://a.example*,*https://b.example*!x",
		},
		{
			// With no emphasis open, the same characters belong to the address.
			name: "URLWithLiteralAsterisksAndNoOpenEmphasis",
			in:   "https://x.example/a**!b and **bold** https://x.example/c**!d",
			want: "https://x.example/a**!b and *bold* https://x.example/c**!d",
		},
		{
			// Inside emphasis, a marker followed by more of the address stays
			// in the address.
			name: "URLWithLiteralAsterisksInsideEmphasis",
			in:   "**see https://x.example/a**b/c now**",
			want: "*see https://x.example/a**b/c now*",
		},
		{
			// Emphasis does not span lines, so an unclosed opener on an
			// earlier line does not cut an address.
			name: "UnclosedEmphasisOnEarlierLineDoesNotCutURL",
			in:   "**open\nhttps://x.example/a**!b",
			want: "**open\nhttps://x.example/a**!b",
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
		{
			// WhatsApp only formats at word boundaries, so URL query params
			// and snake_case ids must come through as typed.
			name: "URLWithUnderscoresUnchanged",
			in:   "🔗 Track: https://acme.example/t?utm_source=wa&utm_medium=tpl",
			want: "🔗 Track: https://acme.example/t?utm_source=wa&utm_medium=tpl",
		},
		{
			// Links are never formatted, even where a path segment is
			// wrapped in delimiters.
			name: "LinkPathsUnchanged",
			in:   "Docs: https://acme.example/_docs_/ and www.acme.example/~john/~doc~",
			want: "Docs: https://acme.example/_docs_/ and www.acme.example/~john/~doc~",
		},
		{
			name: "FormattingAroundLinks",
			in:   "*https://acme.example* _see https://x.example/a_b_c now_",
			want: "**https://acme.example** *see https://x.example/a_b_c now*",
		},
		{
			// Punctuation after the closing delimiter is not part of the link.
			name: "FormattedLinkBeforePunctuation",
			in:   "Kunjungi *https://acme.id/promo*! (_www.acme.id/faq_), ~https://old.acme.id~;",
			want: "Kunjungi **https://acme.id/promo**! (*www.acme.id/faq*), ~~https://old.acme.id~~;",
		},
		{
			// A link starts with a letter, so it is no boundary for a pair.
			name: "PairTouchingLinkUnchanged",
			in:   "*Link:*https://acme.id _note_www.acme.id",
			want: "*Link:*https://acme.id _note_www.acme.id",
		},
		{
			name: "CapitalizedLinkPathsUnchanged",
			in:   "Www.toko.id/_toko_ HTTPS://ACME.ID/_PROMO_",
			want: "Www.toko.id/_toko_ HTTPS://ACME.ID/_PROMO_",
		},
		{
			name: "SnakeCaseUnchanged",
			in:   "Selected option opt_book_now [review_and_pay]",
			want: "Selected option opt_book_now [review_and_pay]",
		},
		{
			name: "MidWordDelimitersUnchanged",
			in:   "2*3*4 and a~b~c",
			want: "2*3*4 and a~b~c",
		},
		{
			// Adjacent pairs share the boundary character between them.
			name: "AdjacentPairs",
			in:   "*a* *b* _c_ _d_ ~e~ ~f~",
			want: "**a** **b** *c* *d* ~~e~~ ~~f~~",
		},
		{
			// Passes are capped at two: an unbounded loop unwraps one nesting
			// level per pass, so a crafted 64KB message burned ~50s of CPU.
			name: "NestingBeyondTwoPassesStops",
			in:   "*a *b *c* d* e*",
			want: "*a **b **c** d** e*",
		},
		{
			// A doubled delimiter is not a WhatsApp pair; leave it as typed.
			name: "DoubledDelimitersUnchanged",
			in:   "**x** ~~y~~ __init__",
			want: "**x** ~~y~~ __init__",
		},
		{
			name: "PunctuationBoundaries",
			in:   "(*Order #42*), _today_!",
			want: "(**Order #42**), *today*!",
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

// TestChatwootToWhatsAppMarkdownHostileInput feeds the converter large inputs
// built to make a careless implementation backtrack or re-scan. Go's regexp
// never backtracks, but ReplaceAll starts a new search after every match, so
// an opener that stays alive across many short matches would still cost one
// scan per match; the inputs below try exactly that for every pattern. Each
// takes about 0.1s (2s under the race detector), where a quadratic pass would
// take minutes, so the bound leaves a loaded CI runner plenty of room.
func TestChatwootToWhatsAppMarkdownHostileInput(t *testing.T) {
	// Chatwoot caps a message at 150,000 characters.
	const size = 150 * 1024
	repeat := func(unit string) string { return strings.Repeat(unit, size/len(unit)) }
	inputs := map[string]string{
		"OpenBrackets":       repeat("["),
		"CloseBrackets":      repeat("]("),
		"OpenParentheses":    repeat("("),
		"NestedBrackets":     repeat("[") + repeat("]"),
		"UnclosedLinks":      repeat("[a](https://x.example/("),
		"LinkInsideOpenLink": repeat("[x](https://a[y](https://b)"),
		"LinkInsideTitle":    repeat("[x](https://a \"[y](https://b)"),
		"LinkThenSpaces":     repeat("[x](   "),
		"ValidLinks":         repeat("[a](https://x.example/) "),
		"Backslashes":        repeat("\\"),
		"Escapes":            repeat("\\*"),
		"OpenFenceEscapes":   "```" + repeat("\\*"),
		"OpenCodeLines":      repeat("`" + strings.Repeat("\\*", 50) + "\n"),
		"Backticks":          repeat("`a"),
		"Asterisks":          repeat("*"),
		"Tildes":             repeat("~"),
		"TripleNesting":      repeat("***a "),
		"BoldOpeners":        repeat("**a "),
		"ItalicOpeners":      repeat(" *a"),
		"ItalicPairs":        repeat("*a*"),
		"MixedEmphasis":      repeat("*a **b* ~~c "),
		"AngleBrackets":      repeat("<"),
		"OpenAutolinks":      repeat("<https://"),
		"OpenEmailAutolinks": repeat("<a@b"),
		"URLStarts":          repeat("http://"),
		"DuplicateLinks":     repeat("https://x.example/a: "),
		"ClosedBoldURLs":     repeat("**https://x.example/a**!"),
		"ReopenedBoldURLs":   "**" + repeat("https://x.example/a**!x**"),
		"OpenBoldURLRun":     "**" + repeat("https://x.example/a**"),
		"DashLines":          repeat("--\n"),
	}
	for name, in := range inputs {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			got := ChatwootToWhatsAppMarkdown(in)
			if elapsed := time.Since(start); elapsed > 30*time.Second {
				t.Fatalf("conversion of %d bytes took %s", len(in), elapsed)
			}
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

// assertNoMarkdownSentinels fails if the markdown bridge leaked any of the
// internal guard runes (\x01 bold, \x02 strike, \x03 link) into a
// user-visible string.
func assertNoMarkdownSentinels(t *testing.T, out string) {
	t.Helper()
	if strings.ContainsAny(out, "\x01\x02\x03") {
		t.Fatalf("output leaked a markdown sentinel rune: %q", out)
	}
}
