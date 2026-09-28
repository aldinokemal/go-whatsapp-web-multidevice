package utils

import (
	"regexp"
	"strings"
)

// MatchesIgnoredJID reports whether jid matches any pattern in the ignore
// list. A pattern is either a suffix wildcard ("@g.us", "@s.whatsapp.net",
// "@lid") that matches an entire address space, or an exact JID. This powers
// config.ChatwootIgnoreJids on both the live-forward and history-import paths.
func MatchesIgnoredJID(jid string, ignore []string) bool {
	if jid == "" {
		return false
	}
	for _, pattern := range ignore {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if strings.HasPrefix(pattern, "@") {
			if strings.HasSuffix(jid, pattern) {
				return true
			}
		} else if jid == pattern {
			return true
		}
	}
	return false
}

// IsSystemBroadcastJID reports the WhatsApp system JIDs that must never reach
// Chatwoot: status@broadcast (the per-account status feed) and 0@s.whatsapp.net
// (WhatsApp's official service account). Forwarding either creates noise
// contacts that pollute the agent inbox without giving an agent anything to
// act on. The match is by exact string — not by suffix — so legitimate JIDs
// like "status@s.whatsapp.net" or "012345@s.whatsapp.net" are not caught.
// Both the live-forward path (webhook_forward.go) and the history importer
// (chatwoot/sync.go) share this single definition.
func IsSystemBroadcastJID(jid string) bool {
	return jid == "status@broadcast" || jid == "0@s.whatsapp.net"
}

// IsNewsletterJID reports whether jid belongs to a WhatsApp channel
// (newsletter). Channels are broadcast feeds with no conversation for an
// agent to handle, and their local part is an 18-digit channel id — not a
// phone number — so Chatwoot's contact creation rejects it with a 422
// ("Phone number should be in e164 format"; E.164 caps at 15 digits).
// Both the live-forward path (webhook_forward.go) and the history importer
// (chatwoot/sync.go) share this single definition.
func IsNewsletterJID(jid string) bool {
	return strings.HasSuffix(jid, "@newsletter")
}

// Markdown translation between WhatsApp and Chatwoot.
//
// WhatsApp renders *bold*, _italic_, ~strikethrough~. Chatwoot renders the
// GitHub-flavored markdown **bold**, *italic*, ~~strikethrough~~. Translating
// in both directions keeps formatting intact as messages cross the bridge.
// Monospace/code spans are intentionally
// left untouched — they are rare in support chats and translating them
// reliably (single vs. triple backtick, nesting) is error-prone.
//
// The transforms use paired, non-greedy delimiters, so a lone unmatched
// delimiter (e.g. "2 * 3") is left alone — exactly the inputs WhatsApp and
// Chatwoot themselves decline to format. Like WhatsApp, the WhatsApp-side
// pairs only count at word boundaries and never inside links, so URLs
// (/_docs_/, ?utm_source=a&utm_medium=b) and snake_case ids pass through
// untouched. Sentinel runes guard the bold pass from being re-matched by the
// italic pass.

const (
	mdBoldSentinel   = "\x01"
	mdStrikeSentinel = "\x02"
	mdLinkSentinel   = "\x03"
)

var (
	reCwBold   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reCwStrike = regexp.MustCompile(`~~(.+?)~~`)
	reCwItalic = regexp.MustCompile(`\*(.+?)\*`)
	// The link sentinel counts as a word character: a link starts with one.
	reWaBold   = regexp.MustCompile(`(^|[^\p{L}\p{N}*\x03])\*([^\s*](?:[^*\n]*[^\s*])?)\*($|[^\p{L}\p{N}*\x03])`)
	reWaItalic = regexp.MustCompile(`(^|[^\p{L}\p{N}_\x03])_([^\s_](?:[^_\n]*[^\s_])?)_($|[^\p{L}\p{N}_\x03])`)
	reWaStrike = regexp.MustCompile(`(^|[^\p{L}\p{N}~\x03])~([^\s~](?:[^~\n]*[^\s~])?)~($|[^\p{L}\p{N}~\x03])`)
	// A trailing delimiter or punctuation stays outside the link, so
	// "*https://x.com*!" still pairs.
	reWaLink = regexp.MustCompile(`(?i:https?://|www\.)\S*[^\s*_~.,;:!?)\]'"]`)
)

// stripMarkdownSentinels removes any pre-existing guard runes from input so
// they can never be mistaken for the internal bold/strike markers and restored
// into markdown. Real WhatsApp/Chatwoot message text never contains these
// control runes, so this is purely defensive.
func stripMarkdownSentinels(s string) string {
	if !strings.ContainsAny(s, mdBoldSentinel+mdStrikeSentinel+mdLinkSentinel) {
		return s
	}
	return strings.NewReplacer(mdBoldSentinel, "", mdStrikeSentinel, "", mdLinkSentinel, "").Replace(s)
}

// ChatwootToWhatsAppMarkdown rewrites Chatwoot/GFM markdown to WhatsApp's
// formatting syntax: **bold**->*bold*, *italic*->_italic_, ~~strike~~->~strike~.
func ChatwootToWhatsAppMarkdown(s string) string {
	if s == "" || !strings.ContainsAny(s, "*~") {
		return s
	}
	s = stripMarkdownSentinels(s)
	// Bold first so its asterisks are hidden from the italic pass.
	s = reCwBold.ReplaceAllString(s, mdBoldSentinel+"$1"+mdBoldSentinel)
	s = reCwStrike.ReplaceAllString(s, mdStrikeSentinel+"$1"+mdStrikeSentinel)
	// "${1}" (braced) is required: a bare "$1_" parses as the named group "1_"
	// (underscore is a legal $name char), which would drop the capture and
	// collapse *italic* to a lone "_".
	s = reCwItalic.ReplaceAllString(s, "_${1}_")
	s = strings.ReplaceAll(s, mdBoldSentinel, "*")
	s = strings.ReplaceAll(s, mdStrikeSentinel, "~")
	return s
}

// WhatsAppToChatwootMarkdown rewrites WhatsApp formatting to Chatwoot/GFM
// markdown: *bold*->**bold**, _italic_->*italic*, ~strike~->~~strike~~.
func WhatsAppToChatwootMarkdown(s string) string {
	if s == "" || !strings.ContainsAny(s, "*_~") {
		return s
	}
	s = stripMarkdownSentinels(s)
	// Hide links behind a sentinel so their delimiters are never paired.
	links := reWaLink.FindAllString(s, -1)
	s = reWaLink.ReplaceAllLiteralString(s, mdLinkSentinel)
	// Hide bold asterisks behind a sentinel, convert italic underscores to
	// asterisks, then expand the sentinel to a double asterisk. Doing it in
	// this order keeps the freshly-created italic asterisks from being seen
	// as bold.
	s = replaceWhatsAppPairs(reWaBold, s, mdBoldSentinel)
	s = replaceWhatsAppPairs(reWaItalic, s, "*")
	s = replaceWhatsAppPairs(reWaStrike, s, mdStrikeSentinel)
	s = strings.ReplaceAll(s, mdBoldSentinel, "**")
	s = strings.ReplaceAll(s, mdStrikeSentinel, "~~")
	if len(links) == 0 {
		return s
	}
	var b strings.Builder
	for i, part := range strings.Split(s, mdLinkSentinel) {
		b.WriteString(part)
		if i < len(links) {
			b.WriteString(links[i])
		}
	}
	return b.String()
}

// replaceWhatsAppPairs swaps the delimiters of every re match for marker. It
// runs twice because adjacent pairs ("*a* *b*") share the boundary character
// between them, which a single pass consumes. It must not loop until stable:
// each extra pass unwraps another nesting level, so crafted input would cost
// one pass per level.
func replaceWhatsAppPairs(re *regexp.Regexp, s, marker string) string {
	repl := "${1}" + marker + "${2}" + marker + "${3}"
	return re.ReplaceAllString(re.ReplaceAllString(s, repl), repl)
}
