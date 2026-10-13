package utils

import (
	"regexp"
	"strconv"
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
//
// On the Chatwoot side a delimiter only counts when it hugs the text it
// formats, as in CommonMark: an opening run is not followed by whitespace and
// a closing run is not preceded by it, so "2 * 3 * 4", "* a bullet" and
// "Price* ... *Terms" stay as written. A single "*" must also sit at a word
// boundary: WhatsApp would not render "30_40_50" as italic, so "30*40*50" is
// better left alone than rewritten.
//
// Chatwoot -> WhatsApp additionally flattens the Markdown that WhatsApp has
// no syntax for, because an API-channel inbox hands over the agent's message
// as stored Markdown and the customer would otherwise read it raw:
//
//	[label](url)         -> label: url   (just the url when the label is the url)
//	<https://x.example/> -> https://x.example/
//	\& \_ \* \. \- \( ... -> the bare character, never read as formatting
//	***bold italic***    -> *_bold italic_*
//	**url**: url         -> url          (how Chatwoot's reply box stores a
//	                                      canned reply holding such a link)
//	trailing "--" line   -> dropped      (signature separator, empty signature)
//
// Code spans, link targets and bare URLs are set aside before the emphasis
// passes and put back verbatim afterwards.

const (
	mdBoldSentinel   = "\x01"
	mdStrikeSentinel = "\x02"
	mdLinkSentinel   = "\x03"
)

var (
	// Chatwoot-side emphasis. The text between a pair starts and ends with a
	// character that is neither whitespace nor the delimiter, which is what
	// keeps "2 * 3 * 4" and a masked "******" from pairing. The inner part is
	// lazy ("??") so the pair closes at the first run that can close it.
	//
	// Bold may hold a nested italic at either end, where the runs touch:
	// "**a *b***" (first alternative: the text ends with a whole "*b*") and
	// "***a* b**" (second alternative: the text starts with "*"). The italic
	// pass pairs the inner asterisks afterwards.
	reCwBold   = regexp.MustCompile(`\*\*((?:[^\s*][^*\n]*)?\*[^\s*](?:[^*\n]*[^\s*])?\*|(?:\*[^\s*]|[^\s*])(?:.*?[^\s*])??)\*\*`)
	reCwStrike = regexp.MustCompile(`~~([^\s~](?:.*?[^\s~])??)~~`)
	// Italic also needs a word boundary on both outer sides, captured as
	// groups 1 and 3 because RE2 has no lookaround (see replaceWhatsAppPairs).
	reCwItalic = regexp.MustCompile(`(^|[^\p{L}\p{N}*])\*([^\s*](?:.*?[^\s*])??)\*($|[^\p{L}\p{N}*])`)
	// The link sentinel counts as a word character: a link starts with one.
	reWaBold   = regexp.MustCompile(`(^|[^\p{L}\p{N}*\x03])\*([^\s*](?:[^*\n]*[^\s*])?)\*($|[^\p{L}\p{N}*\x03])`)
	reWaItalic = regexp.MustCompile(`(^|[^\p{L}\p{N}_\x03])_([^\s_](?:[^_\n]*[^\s_])?)_($|[^\p{L}\p{N}_\x03])`)
	reWaStrike = regexp.MustCompile(`(^|[^\p{L}\p{N}~\x03])~([^\s~](?:[^~\n]*[^\s~])?)~($|[^\p{L}\p{N}~\x03])`)
	// A trailing delimiter or punctuation stays outside the link, so
	// "*https://x.com*!" still pairs.
	reWaLink = regexp.MustCompile(`(?i:https?://|www\.)\S*[^\s*_~.,;:!?)\]'"]`)

	// Text the Chatwoot -> WhatsApp passes must not read as Markdown: fenced
	// and inline code, and backslash escapes. One left-to-right alternation, so
	// an escaped backtick is consumed as an escape and cannot open a code span.
	reCwLiteral = regexp.MustCompile("(?s)```.*?```|`[^`\\n]+`|" + `\\[!-/:-@\[-` + "`{-~]")
	// [label](target "title"), optionally an image. Escapes are already hidden
	// by reCwLiteral, so neither class needs to know about backslashes. The
	// target may hold one level of balanced parentheses (Wikipedia-style).
	reCwInlineLink = regexp.MustCompile(`(?i)!?\[([^\[\]\n]*)\]\(\s*<?((?:https?://|mailto:|tel:)(?:[^()\s<>]|\([^()\s<>]*\))*)>?(?:\s+"[^"\n]*")?\s*\)`)
	reCwAutolink   = regexp.MustCompile(`(?i)<((?:https?://|mailto:|tel:)[^<>\s]+)>`)
	// The link sentinel is allowed in the local part: an escaped "_" is hidden.
	reCwEmailAutolink = regexp.MustCompile(`<([A-Za-z0-9._%+\x03-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,})>`)
	reCwBoldItalic    = regexp.MustCompile(`\*\*\*([^\s*](?:[^*\n]*[^\s*])?)\*\*\*`)
	reCwHidden        = regexp.MustCompile(`\x03(\d+)\x03`)
	reCwLinkScheme    = regexp.MustCompile(`(?i)^(?:mailto|tel):`)
	reCwLinkPrefix    = regexp.MustCompile(`^(?:https?://|mailto:|tel:)`)
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

// cwHidden holds text taken out of a Chatwoot message while it is converted,
// so that later passes cannot read it as Markdown. Each piece is replaced by
// its index between two link sentinels; input sentinels are stripped first, so
// every sentinel in the working text is one of ours.
type cwHidden struct{ parts []string }

// hide stores text and returns its placeholder. text must already be expanded,
// which keeps expand a single pass.
func (h *cwHidden) hide(text string) string {
	h.parts = append(h.parts, text)
	return mdLinkSentinel + strconv.Itoa(len(h.parts)-1) + mdLinkSentinel
}

func (h *cwHidden) expand(s string) string {
	if len(h.parts) == 0 || !strings.Contains(s, mdLinkSentinel) {
		return s
	}
	return reCwHidden.ReplaceAllStringFunc(s, func(m string) string {
		if i, err := strconv.Atoi(m[1 : len(m)-1]); err == nil && i < len(h.parts) {
			return h.parts[i]
		}
		return ""
	})
}

// ChatwootToWhatsAppMarkdown rewrites the Markdown of a Chatwoot agent message
// to text WhatsApp displays as intended: **bold**->*bold*, *italic*->_italic_,
// ~~strike~~->~strike~, ***both***->*_both_*, links and autolinks flattened to
// plain URLs, backslash escapes removed, and the dangling "--" of an empty
// Chatwoot signature dropped. Code and URLs pass through untouched. It returns
// "" when nothing is left to send.
func ChatwootToWhatsAppMarkdown(s string) string {
	if s == "" {
		return s
	}
	if strings.ContainsAny(s, "*~[<\\") || strings.Contains(s, "://") {
		s = convertChatwootMarkdown(stripMarkdownSentinels(s))
	}
	return dropEmptyChatwootSignature(s)
}

// convertChatwootMarkdown runs a fixed number of linear passes; none repeats
// until stable, so the cost stays proportional to the message length.
func convertChatwootMarkdown(s string) string {
	hidden := &cwHidden{}
	s = reCwLiteral.ReplaceAllStringFunc(s, func(m string) string {
		if m[0] == '\\' {
			return hidden.hide(m[1:])
		}
		return hidden.hide(m)
	})
	s = reCwInlineLink.ReplaceAllStringFunc(s, func(m string) string {
		groups := reCwInlineLink.FindStringSubmatch(m)
		label, target := strings.TrimSpace(groups[1]), hidden.expand(groups[2])
		url := hidden.hide(reCwLinkScheme.ReplaceAllString(target, ""))
		if label == "" || comparableLinkText(hidden.expand(label)) == comparableLinkText(target) {
			return url
		}
		return label + ": " + url
	})
	s = reCwAutolink.ReplaceAllStringFunc(s, func(m string) string {
		return hidden.hide(reCwLinkScheme.ReplaceAllString(hidden.expand(m[1:len(m)-1]), ""))
	})
	s = reCwEmailAutolink.ReplaceAllStringFunc(s, func(m string) string {
		return hidden.hide(hidden.expand(m[1 : len(m)-1]))
	})
	s = hideChatwootBareLinks(s, hidden)

	// Bold first so its asterisks are hidden from the italic pass.
	s = reCwBoldItalic.ReplaceAllString(s, mdBoldSentinel+"_${1}_"+mdBoldSentinel)
	s = reCwBold.ReplaceAllString(s, mdBoldSentinel+"$1"+mdBoldSentinel)
	s = reCwStrike.ReplaceAllString(s, mdStrikeSentinel+"$1"+mdStrikeSentinel)
	s = replaceWhatsAppPairs(reCwItalic, s, "_")
	s = strings.ReplaceAll(s, mdBoldSentinel, "*")
	s = strings.ReplaceAll(s, mdStrikeSentinel, "~")
	return hidden.expand(s)
}

// comparableLinkText reduces a link label or target to what a reader sees, to
// tell "[**https://x/**](https://x/)" (label repeats the target) from a real
// label: emphasis markers, case, the scheme and a trailing slash are ignored.
func comparableLinkText(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune("*_~`", r) {
			return -1
		}
		return r
	}, s)
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.TrimRight(reCwLinkPrefix.ReplaceAllString(s, ""), "/")
}

// hideChatwootBareLinks sets every plain URL aside so its asterisks,
// underscores and tildes survive the emphasis passes. A URL followed by ": "
// and the same URL again is Chatwoot's reply box having flattened a link whose
// label was the URL ("**https://x/**: https://x/"); it is emitted once.
func hideChatwootBareLinks(s string, hidden *cwHidden) string {
	locs := reWaLink.FindAllStringIndex(s, -1)
	if len(locs) == 0 {
		return s
	}
	var b strings.Builder
	last := 0
	for i := 0; i < len(locs); i++ {
		start, end := locs[i][0], locs[i][1]
		url := hidden.expand(s[start:end])
		if i+1 < len(locs) {
			next := locs[i+1]
			marker, ok := duplicatedLinkMarker(s[last:start], s[end:next[0]])
			if ok && hidden.expand(s[next[0]:next[1]]) == url {
				start -= len(marker)
				end = next[1]
				i++
			}
		}
		b.WriteString(s[last:start])
		b.WriteString(hidden.hide(url))
		last = end
	}
	b.WriteString(s[last:])
	return b.String()
}

// duplicatedLinkMarker reports whether between (the text separating two URLs)
// is ": " after an optional closing emphasis marker of up to three characters
// that also precedes the first URL, and returns that marker.
func duplicatedLinkMarker(before, between string) (string, bool) {
	marker, ok := strings.CutSuffix(between, ": ")
	if !ok || len(marker) > 3 || strings.Trim(marker, "*_~") != "" {
		return "", false
	}
	return marker, strings.HasSuffix(before, marker)
}

// dropEmptyChatwootSignature removes a last line holding only "--". Chatwoot
// appends "\n\n--\n\n<signature>" for an agent who sends with signature; with
// an empty signature only the separator arrives. A separator that is followed
// by signature text, and "--" inside a sentence, are left alone.
func dropEmptyChatwootSignature(s string) string {
	const blank = " \t\r\n"
	body, ok := strings.CutSuffix(strings.TrimRight(s, blank), "--")
	if !ok {
		return s
	}
	body = strings.TrimRight(body, " \t")
	if body != "" && !strings.HasSuffix(body, "\n") {
		return s
	}
	return strings.TrimRight(body, blank)
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

// replaceWhatsAppPairs swaps the delimiters of every re match for marker. re
// captures the boundary before the pair, its text and the boundary after it.
// It runs twice because adjacent pairs ("*a* *b*") share the boundary character
// between them, which a single pass consumes. It must not loop until stable:
// each extra pass unwraps another nesting level, so crafted input would cost
// one pass per level.
//
// The braced "${2}" matters: a bare "$2_" would be read as the group named
// "2_" and drop the text.
func replaceWhatsAppPairs(re *regexp.Regexp, s, marker string) string {
	repl := "${1}" + marker + "${2}" + marker + "${3}"
	return re.ReplaceAllString(re.ReplaceAllString(s, repl), repl)
}
