package whatsapp

import (
	"context"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
)

// --- Fakes implementing the structural interfaces extractStructuredMessageContent
// matches against. The production code asserts payload values against anonymous
// interfaces (interface{ GetDegreesLatitude()... }), so a test type only needs the
// matching method set — it does NOT need to be the real protobuf type. Each fake is
// given a unique name (ccTest* prefix) to avoid colliding with helpers in sibling
// test files in this package.

// ccTestLocation satisfies the location interface (lat/long/name) used by the
// data["location"] branch.
type ccTestLocation struct {
	lat  float64
	long float64
	name string
}

func (l ccTestLocation) GetDegreesLatitude() float64  { return l.lat }
func (l ccTestLocation) GetDegreesLongitude() float64 { return l.long }
func (l ccTestLocation) GetName() string              { return l.name }

// ccTestLiveLocation satisfies the live_location interface, which deliberately
// lacks GetName — exercising the separate live-location branch.
type ccTestLiveLocation struct {
	lat  float64
	long float64
}

func (l ccTestLiveLocation) GetDegreesLatitude() float64  { return l.lat }
func (l ccTestLiveLocation) GetDegreesLongitude() float64 { return l.long }

// ccTestList satisfies the list interface (GetTitle).
type ccTestList struct{ title string }

func (l ccTestList) GetTitle() string { return l.title }

// ccTestOrder satisfies the order interface (GetOrderTitle).
type ccTestOrder struct{ title string }

func (o ccTestOrder) GetOrderTitle() string { return o.title }

// ccTestContactIface satisfies the GetDisplayName/GetVcard interface branch in
// extractContactDetails — the path the real waE2E.ContactMessage takes.
type ccTestContactIface struct {
	name  string
	vcard string
}

func (c ccTestContactIface) GetDisplayName() string { return c.name }
func (c ccTestContactIface) GetVcard() string       { return c.vcard }

// vcardWithTel builds a minimal vCard whose TEL field carries the supplied number,
// so tests can drive ExtractPhoneFromVCard without hand-writing the format inline.
func vcardWithTel(t *testing.T, phone string) string {
	t.Helper()
	return "BEGIN:VCARD\nVERSION:3.0\nFN:Someone\nTEL;type=CELL:" + phone + "\nEND:VCARD"
}

// TestExtractChatwootContactInfo pins the contact-derivation logic that decides
// which Chatwoot contact a WhatsApp message maps to. The branch selection here is
// security/UX sensitive: a wrong identifier silently merges or splits conversations
// in the agent inbox, and a missed system-JID guard floods Chatwoot with status
// noise. Every case calls with context.Background(), where getGroupName has no
// WhatsApp client and therefore returns "" — exercising the group-name fallback.
func TestExtractChatwootContactInfo(t *testing.T) {
	ctx := context.Background()

	t.Run("empty from is rejected", func(t *testing.T) {
		// Without a sender we cannot identify a contact at all; the producer must
		// never reach Chatwoot, so this is an error rather than a placeholder.
		info, err := extractChatwootContactInfo(ctx, map[string]any{"from": ""})
		if err == nil {
			t.Fatalf("expected error for empty 'from', got info=%+v", info)
		}
		if info != nil {
			t.Fatalf("expected nil info on error, got %+v", info)
		}
	})

	t.Run("missing from field is rejected", func(t *testing.T) {
		// A payload with no 'from' key at all takes the same path as empty string
		// (the type assertion yields "").
		if _, err := extractChatwootContactInfo(ctx, map[string]any{}); err == nil {
			t.Fatal("expected error when 'from' field is absent")
		}
	})

	t.Run("status broadcast chat_id is skipped", func(t *testing.T) {
		// status@broadcast in chat_id must short-circuit even though 'from' is a
		// real user — relaying status posts would spawn a noise "Status" contact.
		_, err := extractChatwootContactInfo(ctx, map[string]any{
			"from":    "628111@s.whatsapp.net",
			"chat_id": "status@broadcast",
		})
		if err == nil {
			t.Fatal("expected status@broadcast chat_id to be skipped")
		}
	})

	t.Run("system service account from is skipped", func(t *testing.T) {
		// 0@s.whatsapp.net is WhatsApp's official service account; guarding on
		// 'from' (not just chat_id) blocks its TOS/notice messages.
		_, err := extractChatwootContactInfo(ctx, map[string]any{
			"from": "0@s.whatsapp.net",
		})
		if err == nil {
			t.Fatal("expected 0@s.whatsapp.net 'from' to be skipped")
		}
	})

	t.Run("group uses chat_id identifier and Group: phone fallback name", func(t *testing.T) {
		// For a group, the identifier is the raw @g.us JID (so every member's
		// messages collapse into one conversation). With no client in context
		// getGroupName returns "", so Name falls back to "Group: <extracted phone>".
		groupJID := "120363999000111@g.us"
		// Seed the cache with an empty name so the fallback is deterministic even
		// if another test in this package has set a global WhatsApp client. A
		// cached "" still flows to the "Group: <phone>" fallback. This is a fresh
		// entry (TTL in the future), so no timing dependency is introduced.
		setCachedGroupName(groupJID, "")
		info, err := extractChatwootContactInfo(ctx, map[string]any{
			"from":      "628111@s.whatsapp.net",
			"chat_id":   groupJID,
			"from_name": "Alice",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !info.IsGroup {
			t.Fatal("expected IsGroup=true for @g.us chat_id")
		}
		if info.Identifier != groupJID {
			t.Fatalf("expected identifier %q, got %q", groupJID, info.Identifier)
		}
		// ExtractPhoneFromJID splits on '@', so the fallback embeds the JID's local part.
		wantName := "Group: 120363999000111"
		if info.Name != wantName {
			t.Fatalf("expected fallback name %q, got %q", wantName, info.Name)
		}
		// FromName is carried through verbatim for later sender-prefixing.
		if info.FromName != "Alice" {
			t.Fatalf("expected FromName=Alice, got %q", info.FromName)
		}
	})

	t.Run("group fallback prefix even when from_name empty", func(t *testing.T) {
		groupJID := "120363222333444@g.us"
		setCachedGroupName(groupJID, "")
		info, err := extractChatwootContactInfo(ctx, map[string]any{
			"from":    "628111@s.whatsapp.net",
			"chat_id": groupJID,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasPrefix(info.Name, "Group: ") {
			t.Fatalf("expected name to start with 'Group: ', got %q", info.Name)
		}
	})

	t.Run("outgoing 1:1 uses chat_id identifier and identifier as name", func(t *testing.T) {
		// is_from_me=true means WE sent it; the contact is the recipient encoded in
		// chat_id, and there is no pushname for ourselves, so Name == Identifier.
		info, err := extractChatwootContactInfo(ctx, map[string]any{
			"from":       "628000@s.whatsapp.net",
			"chat_id":    "628999@s.whatsapp.net",
			"is_from_me": true,
			"from_name":  "Me",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if info.IsGroup {
			t.Fatal("expected IsGroup=false for s.whatsapp.net chat")
		}
		// chatwootIdentifierForJID strips the suffix for ordinary phone JIDs.
		if info.Identifier != "628999" {
			t.Fatalf("expected identifier 628999 (from chat_id), got %q", info.Identifier)
		}
		if info.Name != info.Identifier {
			t.Fatalf("expected Name==Identifier for outgoing, got Name=%q Identifier=%q", info.Name, info.Identifier)
		}
	})

	t.Run("inbound 1:1 uses from identifier and from_name as name", func(t *testing.T) {
		// Incoming 1:1: identifier derives from 'from', and the human-readable
		// pushname (from_name) becomes the contact Name when present.
		info, err := extractChatwootContactInfo(ctx, map[string]any{
			"from":      "628123456789@s.whatsapp.net",
			"chat_id":   "628123456789@s.whatsapp.net",
			"from_name": "Charlie",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if info.Identifier != "628123456789" {
			t.Fatalf("expected identifier 628123456789, got %q", info.Identifier)
		}
		if info.Name != "Charlie" {
			t.Fatalf("expected Name=Charlie, got %q", info.Name)
		}
	})

	t.Run("inbound 1:1 falls back to identifier when from_name empty", func(t *testing.T) {
		// No pushname → Name defaults to the identifier so the inbox isn't blank.
		info, err := extractChatwootContactInfo(ctx, map[string]any{
			"from":    "628123456789@s.whatsapp.net",
			"chat_id": "628123456789@s.whatsapp.net",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if info.Name != "628123456789" {
			t.Fatalf("expected Name to fall back to identifier, got %q", info.Name)
		}
	})

	t.Run("lid sender keeps @lid suffix in identifier", func(t *testing.T) {
		// @lid (privacy-masked) JIDs must retain the suffix so the Chatwoot client
		// takes its identifier-search branch instead of phone-normalizing garbage.
		info, err := extractChatwootContactInfo(ctx, map[string]any{
			"from":      "1234567890abcd@lid",
			"chat_id":   "1234567890abcd@lid",
			"from_name": "Masked",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if info.Identifier != "1234567890abcd@lid" {
			t.Fatalf("expected @lid identifier preserved, got %q", info.Identifier)
		}
		if info.Name != "Masked" {
			t.Fatalf("expected Name=Masked, got %q", info.Name)
		}
	})
}

// TestBuildChatwootMessageContent pins how a message payload is rendered into the
// (content, attachments) pair Chatwoot receives. The media handling and group
// sender-prefixing are the regression-prone parts: a dropped attachment or a
// missing sender prefix degrades the agent experience silently.
func TestBuildChatwootMessageContent(t *testing.T) {
	t.Run("plain body passthrough", func(t *testing.T) {
		content, atts := buildChatwootMessageContent(map[string]any{"body": "hello world"}, false, "")
		if content != "hello world" {
			t.Fatalf("expected body passthrough, got %q", content)
		}
		if len(atts) != 0 {
			t.Fatalf("expected no attachments, got %v", atts)
		}
	})

	t.Run("empty body falls back to structured content", func(t *testing.T) {
		// An empty body but a structured location → the structured extractor fills
		// the content instead of leaving it blank.
		content, _ := buildChatwootMessageContent(map[string]any{
			"body":     "",
			"location": ccTestLocation{lat: 1.5, long: 2.5, name: "Office"},
		}, false, "")
		if content != "Location: Office (1.500000, 2.500000)" {
			t.Fatalf("expected structured location content, got %q", content)
		}
	})

	t.Run("group prefixes sender name onto text content", func(t *testing.T) {
		// In a group, every text message is prefixed "<sender>: " so agents can tell
		// who spoke without opening each message.
		content, _ := buildChatwootMessageContent(map[string]any{"body": "morning"}, true, "Alice")
		if content != "Alice: morning" {
			t.Fatalf("expected 'Alice: morning', got %q", content)
		}
	})

	t.Run("group falls back to participant phone when fromName empty", func(t *testing.T) {
		// With no pushname but a participant JID present, the sender is labeled by
		// phone so group attribution survives, matching the reaction path.
		content, _ := buildChatwootMessageContent(map[string]any{
			"body": "hi",
			"from": "628111@s.whatsapp.net",
		}, true, "")
		if content != "628111: hi" {
			t.Fatalf("expected '628111: hi', got %q", content)
		}
	})

	t.Run("group with empty fromName and no from does not prefix", func(t *testing.T) {
		// Guard: nothing to attribute → no stray ": " prefix.
		content, _ := buildChatwootMessageContent(map[string]any{"body": "hi"}, true, "")
		if content != "hi" {
			t.Fatalf("expected unprefixed 'hi', got %q", content)
		}
	})

	t.Run("outgoing group message is not prefixed with operator name", func(t *testing.T) {
		// is_from_me messages are our own replies; prefixing them with the
		// operator's pushname would mislabel the outgoing bubble.
		content, _ := buildChatwootMessageContent(map[string]any{
			"body":       "on it",
			"is_from_me": true,
		}, true, "Operator")
		if content != "on it" {
			t.Fatalf("expected unprefixed 'on it' for outgoing, got %q", content)
		}
	})

	// Each media field is verified for both shapes the producer can emit:
	// a bare string path, and a {path, caption} map. Both must yield exactly one
	// attachment. A url-only map (auto-download disabled) must yield none.
	mediaFields := []string{"image", "audio", "video", "document", "sticker", "video_note"}
	for _, field := range mediaFields {
		field := field
		t.Run("media field string path -> attachment: "+field, func(t *testing.T) {
			content, atts := buildChatwootMessageContent(map[string]any{
				field: "/tmp/wa/" + field + ".bin",
			}, false, "")
			if len(atts) != 1 || atts[0] != "/tmp/wa/"+field+".bin" {
				t.Fatalf("expected single attachment for %s string, got %v", field, atts)
			}
			// With an attachment present, the placeholder is NOT applied (that only
			// fires when there is neither content nor any attachment). For a
			// non-group media-only message the content stays empty — Chatwoot shows
			// just the file.
			if content != "" {
				t.Fatalf("expected empty content for media-only %s, got %q", field, content)
			}
		})

		t.Run("media field {path,caption} map -> attachment + caption: "+field, func(t *testing.T) {
			content, atts := buildChatwootMessageContent(map[string]any{
				"body": "see this",
				field:  map[string]any{"path": "/tmp/wa/" + field + ".bin", "caption": "see this"},
			}, false, "")
			if len(atts) != 1 || atts[0] != "/tmp/wa/"+field+".bin" {
				t.Fatalf("expected single attachment for %s map, got %v", field, atts)
			}
			if content != "see this" {
				t.Fatalf("expected caption body for %s, got %q", field, content)
			}
		})

		t.Run("media field url-only map -> no attachment: "+field, func(t *testing.T) {
			// auto-download disabled: only a remote URL is present, which Chatwoot
			// can't fetch, so it must not be added as an attachment.
			content, atts := buildChatwootMessageContent(map[string]any{
				field: map[string]any{"url": "https://wa.example/" + field, "caption": "x"},
			}, false, "")
			if len(atts) != 0 {
				t.Fatalf("expected no attachment for url-only %s, got %v", field, atts)
			}
			if content != "(Unsupported message type)" {
				t.Fatalf("expected placeholder when only url present for %s, got %q", field, content)
			}
		})
	}

	t.Run("nil media value is skipped", func(t *testing.T) {
		// A present-but-nil field must be ignored, not panic or add a blank attachment.
		_, atts := buildChatwootMessageContent(map[string]any{"image": nil}, false, "")
		if len(atts) != 0 {
			t.Fatalf("expected no attachment for nil image, got %v", atts)
		}
	})

	t.Run("no body and no attachments yields placeholder", func(t *testing.T) {
		content, atts := buildChatwootMessageContent(map[string]any{}, false, "")
		if content != "(Unsupported message type)" {
			t.Fatalf("expected placeholder, got %q", content)
		}
		if len(atts) != 0 {
			t.Fatalf("expected no attachments, got %v", atts)
		}
	})

	t.Run("group with attachment but empty content prepends sender (media)", func(t *testing.T) {
		// A group media message with no text still credits the sender via the
		// "<sender>: (media)" form so the inbox shows attribution.
		content, atts := buildChatwootMessageContent(map[string]any{
			"image": "/tmp/wa/photo.jpg",
		}, true, "Alice")
		if len(atts) != 1 {
			t.Fatalf("expected one attachment, got %v", atts)
		}
		if content != "Alice: (media)" {
			t.Fatalf("expected 'Alice: (media)', got %q", content)
		}
	})

	t.Run("group with attachment and text prefixes sender onto text", func(t *testing.T) {
		// When both a caption and media exist in a group, the caption is prefixed
		// with the sender and the file is still attached.
		content, atts := buildChatwootMessageContent(map[string]any{
			"body":  "nice pic",
			"image": map[string]any{"path": "/tmp/wa/photo.jpg", "caption": "nice pic"},
		}, true, "Alice")
		if len(atts) != 1 {
			t.Fatalf("expected one attachment, got %v", atts)
		}
		if content != "Alice: nice pic" {
			t.Fatalf("expected 'Alice: nice pic', got %q", content)
		}
	})

	t.Run("multiple media fields all attach", func(t *testing.T) {
		// The loop walks every media field, so a payload carrying several produces
		// one attachment each, in field order.
		_, atts := buildChatwootMessageContent(map[string]any{
			"image":    "/tmp/wa/a.jpg",
			"audio":    "/tmp/wa/b.oga",
			"document": "/tmp/wa/c.pdf",
		}, false, "")
		if len(atts) != 3 {
			t.Fatalf("expected 3 attachments, got %v", atts)
		}
	})
}

// TestExtractStructuredMessageContentVariants drives every non-text branch of the
// structured extractor. The function is what renders non-body messages (contacts,
// locations, lists, orders) into a human-readable line, so each branch's exact
// output is part of the agent-facing contract. Note: the contact and contacts-array
// happy paths via webhookContactPayload are already covered in webhook_forward_test.go
// — here we focus on the map/interface/empty/default permutations and the
// location/list/order branches.
func TestExtractStructuredMessageContentVariants(t *testing.T) {
	t.Run("contact map with displayName and phone", func(t *testing.T) {
		got := extractStructuredMessageContent(map[string]any{
			"contact": map[string]any{
				"displayName":  "Dana",
				"phone_number": "+62 800 1",
			},
		})
		if got != "Contact: Dana (+62 800 1)" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("contact map with only vcard extracts phone", func(t *testing.T) {
		// No phone_number key → fall back to parsing the vCard's TEL field.
		got := extractStructuredMessageContent(map[string]any{
			"contact": map[string]any{
				"displayName": "Eve",
				"vcard":       vcardWithTel(t, "+628999"),
			},
		})
		if got != "Contact: Eve (+628999)" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("contact map with no usable fields yields Contact shared", func(t *testing.T) {
		// extractContactDetails returns ok=false (no name, no phone), so the
		// extractor uses the generic "Contact shared" sentinel.
		got := extractStructuredMessageContent(map[string]any{
			"contact": map[string]any{"unrelated": "x"},
		})
		if got != "Contact shared" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("contact via GetDisplayName/GetVcard interface", func(t *testing.T) {
		// The real protobuf ContactMessage takes this branch; the phone comes from
		// the vCard TEL field.
		got := extractStructuredMessageContent(map[string]any{
			"contact": ccTestContactIface{name: "Frank", vcard: vcardWithTel(t, "+628111")},
		})
		if got != "Contact: Frank (+628111)" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("contacts_array []any non-empty uses first with plural prefix", func(t *testing.T) {
		// JSON-decoded payloads arrive as []any; the first element drives the
		// summary and the plural "Contacts" prefix is used.
		got := extractStructuredMessageContent(map[string]any{
			"contacts_array": []any{
				map[string]any{"displayName": "Gina", "phone_number": "+62 1"},
				map[string]any{"displayName": "Hank", "phone_number": "+62 2"},
			},
		})
		if got != "Contacts: Gina (+62 1)" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("contacts_array []any empty yields Contacts shared", func(t *testing.T) {
		got := extractStructuredMessageContent(map[string]any{
			"contacts_array": []any{},
		})
		if got != "Contacts shared" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("contacts_array []any first element unusable yields Contacts shared", func(t *testing.T) {
		// First element has no extractable name/phone → fall back to sentinel.
		got := extractStructuredMessageContent(map[string]any{
			"contacts_array": []any{map[string]any{"unrelated": "x"}},
		})
		if got != "Contacts shared" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("contacts_array pointer slice normalizes and skips nil", func(t *testing.T) {
		// []*webhookContactPayload is normalized to values; a nil pointer is dropped
		// so the first non-nil entry drives the summary.
		got := extractStructuredMessageContent(map[string]any{
			"contacts_array": []*webhookContactPayload{
				nil,
				{DisplayName: "Ivy", PhoneNumber: "+62 9"},
			},
		})
		if got != "Contacts: Ivy (+62 9)" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("contacts_array pointer slice all nil yields Contacts shared", func(t *testing.T) {
		// Every pointer nil → normalized slice empty → sentinel.
		got := extractStructuredMessageContent(map[string]any{
			"contacts_array": []*webhookContactPayload{nil, nil},
		})
		if got != "Contacts shared" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("contacts_array unknown type yields Contacts shared", func(t *testing.T) {
		// The default switch arm guards against an unexpected concrete type.
		got := extractStructuredMessageContent(map[string]any{
			"contacts_array": "just a string",
		})
		if got != "Contacts shared" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("location with name", func(t *testing.T) {
		got := extractStructuredMessageContent(map[string]any{
			"location": ccTestLocation{lat: -6.2, long: 106.8, name: "HQ"},
		})
		if got != "Location: HQ (-6.200000, 106.800000)" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("location without name omits the label", func(t *testing.T) {
		// Empty name → the coordinates-only form (no "Name (" wrapper).
		got := extractStructuredMessageContent(map[string]any{
			"location": ccTestLocation{lat: -6.2, long: 106.8},
		})
		if got != "Location: -6.200000, 106.800000" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("location value not implementing interface yields generic", func(t *testing.T) {
		// A non-nil location that does not satisfy the interface still produces a
		// readable sentinel rather than crashing.
		got := extractStructuredMessageContent(map[string]any{
			"location": "raw string location",
		})
		if got != "Location shared" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("live_location", func(t *testing.T) {
		got := extractStructuredMessageContent(map[string]any{
			"live_location": ccTestLiveLocation{lat: 1.1, long: 2.2},
		})
		if got != "Live Location: 1.100000, 2.200000" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("live_location not implementing interface yields generic", func(t *testing.T) {
		got := extractStructuredMessageContent(map[string]any{
			"live_location": 123,
		})
		if got != "Live location shared" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("list with title", func(t *testing.T) {
		got := extractStructuredMessageContent(map[string]any{
			"list": ccTestList{title: "Menu"},
		})
		if got != "List: Menu" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("list with empty title yields generic", func(t *testing.T) {
		// Implements the interface but title is empty → generic "List message".
		got := extractStructuredMessageContent(map[string]any{
			"list": ccTestList{title: ""},
		})
		if got != "List message" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("list not implementing interface yields generic", func(t *testing.T) {
		got := extractStructuredMessageContent(map[string]any{
			"list": "raw",
		})
		if got != "List message" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("order with title", func(t *testing.T) {
		got := extractStructuredMessageContent(map[string]any{
			"order": ccTestOrder{title: "Pizza"},
		})
		if got != "Order: Pizza" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("order with empty title yields generic", func(t *testing.T) {
		got := extractStructuredMessageContent(map[string]any{
			"order": ccTestOrder{title: ""},
		})
		if got != "Order message" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("order not implementing interface yields generic", func(t *testing.T) {
		got := extractStructuredMessageContent(map[string]any{
			"order": 42,
		})
		if got != "Order message" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("nil contact value falls through to next branch", func(t *testing.T) {
		// A present "contact" key holding nil must be treated as absent (the
		// `&& contact != nil` guard), so a sibling location branch still wins.
		got := extractStructuredMessageContent(map[string]any{
			"contact":  nil,
			"location": ccTestLocation{lat: 1, long: 2, name: "X"},
		})
		if got != "Location: X (1.000000, 2.000000)" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("empty payload yields empty string", func(t *testing.T) {
		// No recognized structured key → "" so the caller falls back to its own
		// placeholder logic.
		if got := extractStructuredMessageContent(map[string]any{}); got != "" {
			t.Fatalf("expected empty string, got %q", got)
		}
	})
}

// TestExtractContactDetails pins the per-shape contact parsing used by the
// structured extractor. Each concrete type the payload can carry maps to a
// (name, phone, ok) triple; the ok flag gates whether the caller renders a
// formatted summary or a generic sentinel.
func TestExtractContactDetails(t *testing.T) {
	t.Run("webhookContactPayload value", func(t *testing.T) {
		name, phone, ok := extractContactDetails(webhookContactPayload{
			DisplayName: "Alice", PhoneNumber: "+62 1",
		})
		if !ok || name != "Alice" || phone != "+62 1" {
			t.Fatalf("got name=%q phone=%q ok=%v", name, phone, ok)
		}
	})

	t.Run("webhookContactPayload pointer", func(t *testing.T) {
		name, phone, ok := extractContactDetails(&webhookContactPayload{
			DisplayName: "Bob", PhoneNumber: "+62 2",
		})
		if !ok || name != "Bob" || phone != "+62 2" {
			t.Fatalf("got name=%q phone=%q ok=%v", name, phone, ok)
		}
	})

	t.Run("nil webhookContactPayload pointer is not ok", func(t *testing.T) {
		// A typed-nil pointer must report ok=false rather than dereferencing.
		var p *webhookContactPayload
		name, phone, ok := extractContactDetails(p)
		if ok || name != "" || phone != "" {
			t.Fatalf("expected empty/false, got name=%q phone=%q ok=%v", name, phone, ok)
		}
	})

	t.Run("map with displayName key", func(t *testing.T) {
		name, phone, ok := extractContactDetails(map[string]any{
			"displayName": "Cara", "phone_number": "+62 3",
		})
		if !ok || name != "Cara" || phone != "+62 3" {
			t.Fatalf("got name=%q phone=%q ok=%v", name, phone, ok)
		}
	})

	t.Run("map with display_name fallback key", func(t *testing.T) {
		// When camelCase displayName is absent, snake_case display_name is read.
		name, _, ok := extractContactDetails(map[string]any{
			"display_name": "Dee",
		})
		if !ok || name != "Dee" {
			t.Fatalf("got name=%q ok=%v", name, ok)
		}
	})

	t.Run("map displayName takes precedence over display_name", func(t *testing.T) {
		// The else-if order means displayName wins when both are present.
		name, _, _ := extractContactDetails(map[string]any{
			"displayName":  "Primary",
			"display_name": "Secondary",
		})
		if name != "Primary" {
			t.Fatalf("expected displayName precedence, got %q", name)
		}
	})

	t.Run("map falls back to vcard when phone_number absent", func(t *testing.T) {
		name, phone, ok := extractContactDetails(map[string]any{
			"displayName": "Erin",
			"vcard":       vcardWithTel(t, "+628222"),
		})
		if !ok || name != "Erin" || phone != "+628222" {
			t.Fatalf("got name=%q phone=%q ok=%v", name, phone, ok)
		}
	})

	t.Run("map phone_number wins over vcard", func(t *testing.T) {
		// vcard is only consulted when phone_number resolves to empty.
		_, phone, _ := extractContactDetails(map[string]any{
			"phone_number": "+62 explicit",
			"vcard":        vcardWithTel(t, "+62 fromcard"),
		})
		if phone != "+62 explicit" {
			t.Fatalf("expected explicit phone, got %q", phone)
		}
	})

	t.Run("empty map is not ok", func(t *testing.T) {
		// Neither name nor phone → ok=false.
		_, _, ok := extractContactDetails(map[string]any{})
		if ok {
			t.Fatal("expected ok=false for empty map")
		}
	})

	t.Run("GetDisplayName/GetVcard interface", func(t *testing.T) {
		name, phone, ok := extractContactDetails(ccTestContactIface{
			name: "Fay", vcard: vcardWithTel(t, "+628333"),
		})
		if !ok || name != "Fay" || phone != "+628333" {
			t.Fatalf("got name=%q phone=%q ok=%v", name, phone, ok)
		}
	})

	t.Run("unrelated type is not ok", func(t *testing.T) {
		// The default switch arm rejects anything that doesn't match a known shape.
		_, _, ok := extractContactDetails(12345)
		if ok {
			t.Fatal("expected ok=false for unrelated type")
		}
	})
}

// TestStructuredContactsArraySummary pins the small helper that summarizes a typed
// contacts slice — empty slices must not index out of bounds.
func TestStructuredContactsArraySummary(t *testing.T) {
	t.Run("empty slice yields Contacts shared", func(t *testing.T) {
		if got := structuredContactsArraySummary(nil); got != "Contacts shared" {
			t.Fatalf("got %q", got)
		}
		if got := structuredContactsArraySummary([]webhookContactPayload{}); got != "Contacts shared" {
			t.Fatalf("got %q for empty slice", got)
		}
	})

	t.Run("non-empty uses first contact with plural prefix", func(t *testing.T) {
		got := structuredContactsArraySummary([]webhookContactPayload{
			{DisplayName: "Gail", PhoneNumber: "+62 7"},
			{DisplayName: "Hugo", PhoneNumber: "+62 8"},
		})
		if got != "Contacts: Gail (+62 7)" {
			t.Fatalf("got %q", got)
		}
	})
}

// TestIsEventWhitelisted pins the case-insensitive, space-trimming whitelist match.
// It mutates the global config slice, so it saves and restores the original value.
func TestIsEventWhitelisted(t *testing.T) {
	original := config.WhatsappWebhookEvents
	defer func() { config.WhatsappWebhookEvents = original }()

	t.Run("exact match", func(t *testing.T) {
		config.WhatsappWebhookEvents = []string{"message"}
		if !isEventWhitelisted("message") {
			t.Fatal("expected exact match to be whitelisted")
		}
	})

	t.Run("case-insensitive match", func(t *testing.T) {
		config.WhatsappWebhookEvents = []string{"MESSAGE"}
		if !isEventWhitelisted("message") {
			t.Fatal("expected case-insensitive match")
		}
	})

	t.Run("surrounding whitespace is trimmed", func(t *testing.T) {
		// Operators often paste comma-separated lists with stray spaces; the match
		// trims each configured entry before comparing.
		config.WhatsappWebhookEvents = []string{"  message.ack  "}
		if !isEventWhitelisted("message.ack") {
			t.Fatal("expected whitespace-trimmed match")
		}
	})

	t.Run("absent event is not whitelisted", func(t *testing.T) {
		config.WhatsappWebhookEvents = []string{"message"}
		if isEventWhitelisted("message.reaction") {
			t.Fatal("expected unlisted event to be rejected")
		}
	})

	t.Run("empty whitelist matches nothing here", func(t *testing.T) {
		// isEventWhitelisted itself does NOT treat an empty list as allow-all — that
		// allow-all behavior lives in the callers. With no entries the loop never
		// matches.
		config.WhatsappWebhookEvents = nil
		if isEventWhitelisted("message") {
			t.Fatal("expected empty whitelist to match nothing in isEventWhitelisted")
		}
	})
}

// TestGroupNameCache pins the TTL cache's store/load contract without depending on
// expiry timing. setCachedGroupName stores with a future TTL, so a subsequent load
// returns (name, true); an unknown key returns ("", false).
func TestGroupNameCache(t *testing.T) {
	t.Run("store then load returns cached name", func(t *testing.T) {
		// Use a JID unlikely to collide with other tests so a leftover entry from a
		// prior run can't mask a regression.
		jid := "cachetest-store@g.us"
		setCachedGroupName(jid, "My Group")
		name, ok := getCachedGroupName(jid)
		if !ok || name != "My Group" {
			t.Fatalf("expected ('My Group', true), got (%q, %v)", name, ok)
		}
	})

	t.Run("unknown key returns empty and false", func(t *testing.T) {
		name, ok := getCachedGroupName("cachetest-missing@g.us")
		if ok || name != "" {
			t.Fatalf("expected ('', false) for unknown key, got (%q, %v)", name, ok)
		}
	})

	t.Run("empty cached name still reports present", func(t *testing.T) {
		// Storing an empty name is meaningful (it records "we tried and got nothing")
		// — load must still return ok=true so callers don't re-fetch.
		jid := "cachetest-empty@g.us"
		setCachedGroupName(jid, "")
		name, ok := getCachedGroupName(jid)
		if !ok || name != "" {
			t.Fatalf("expected ('', true) for empty cached name, got (%q, %v)", name, ok)
		}
	})
}
