package chatwoot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
)

type Client struct {
	BaseURL    string
	APIToken   string
	AccountID  int
	InboxID    int
	HTTPClient *http.Client

	// InboxIdentifier is the public inbox token used by the contact-facing
	// endpoints (e.g. update_last_seen). It is immutable for a given inbox, so
	// it is resolved once (at provisioning or on first use) and cached here to
	// avoid a full inbox-list GET on every read receipt.
	InboxIdentifier string
}

// HTTPStatusError is returned by every Client method that hits Chatwoot's
// REST API when the response status is outside 2xx. It carries the HTTP
// status code so callers (notably retrySyncOp in sync.go) can decide
// whether to retry: 429 and 5xx are transient, 4xx is not.
type HTTPStatusError struct {
	StatusCode int
	Op         string
	Body       string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("chatwoot %s: status %d body %s", e.Op, e.StatusCode, e.Body)
}

// Retryable reports whether an error represents a transient failure that
// is worth retrying. Network/IO errors that reach callers without a status
// code (e.g. connection refused, timeout) are always retryable; HTTP
// responses are retryable only for 429 and 5xx.
func Retryable(err error) bool {
	if err == nil {
		return false
	}
	var httpErr *HTTPStatusError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode == http.StatusTooManyRequests || httpErr.StatusCode >= 500
	}
	// Non-HTTP error: network, timeout, json encode, etc. — retry.
	return true
}

var (
	defaultClient     *Client
	defaultClientOnce sync.Once

	// sentMessageIDs tracks Chatwoot message IDs created by our API to prevent
	// echo loops: WhatsApp msg → synced to Chatwoot → Chatwoot webhook fires →
	// would re-send to WhatsApp without this guard.
	sentMessageIDs    sync.Map
	sentMessageIDsTTL = 5 * time.Minute
)

// GetDefaultClient returns a shared Chatwoot client instance.
// Uses lazy initialization with sync.Once for thread safety.
func GetDefaultClient() *Client {
	defaultClientOnce.Do(func() {
		defaultClient = NewClient()
	})
	return defaultClient
}

func MarkMessageAsSent(messageID int) {
	if messageID == 0 {
		return
	}
	sentMessageIDs.Store(messageID, time.Now())
}

func IsMessageSentByUs(messageID int) bool {
	if messageID == 0 {
		return false
	}
	val, ok := sentMessageIDs.Load(messageID)
	if !ok {
		return false
	}
	storedAt := val.(time.Time)
	if time.Since(storedAt) > sentMessageIDsTTL {
		sentMessageIDs.Delete(messageID)
		return false
	}
	// Don't delete on check — Chatwoot may fire multiple webhook events
	// (e.g. message_created + conversation_updated) for the same message.
	// Entries are cleaned up by the background sweeper after TTL expires.
	return true
}

func init() {
	go func() {
		ticker := time.NewTicker(sentMessageIDsTTL)
		defer ticker.Stop()
		for range ticker.C {
			sentMessageIDs.Range(func(key, value any) bool {
				if time.Since(value.(time.Time)) > sentMessageIDsTTL {
					sentMessageIDs.Delete(key)
				}
				return true
			})
		}
	}()
}

func NewClient() *Client {
	// Trim surrounding whitespace before normalizing. Tokens and URLs supplied
	// via Docker secret files, .env lines, or shell heredocs routinely carry a
	// trailing newline; an untrimmed token produces a malformed
	// "api_access_token" header and Chatwoot answers every request with a 401
	// ("You need to sign in or sign up before continuing"), and a trailing
	// newline on the URL survives the slash trim and corrupts every endpoint.
	return &Client{
		BaseURL:   strings.TrimRight(strings.TrimSpace(config.ChatwootURL), "/"),
		APIToken:  strings.TrimSpace(config.ChatwootAPIToken),
		AccountID: config.ChatwootAccountID,
		InboxID:   config.ChatwootInboxID,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) IsConfigured() bool {
	return c.BaseURL != "" && c.APIToken != "" && c.AccountID != 0 && c.InboxID != 0
}

func (c *Client) FindContactByIdentifier(identifier string, isGroup bool) (*Contact, error) {
	endpoint := fmt.Sprintf("%s/api/v1/accounts/%d/contacts/search", c.BaseURL, c.AccountID)
	logrus.Debugf("Chatwoot: Finding contact by identifier endpoint=%s identifier=%s isGroup=%v", endpoint, identifier, isGroup)
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	searchTerm := identifier
	isIdentifierBased := isGroup || strings.HasSuffix(identifier, "@lid")
	if !isIdentifierBased {
		searchTerm = utils.NormalizePhoneE164(identifier)
	}

	q := req.URL.Query()
	q.Add("q", searchTerm)
	req.URL.RawQuery = q.Encode()
	req.Header.Set("api_access_token", c.APIToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode, Op: "search contact", Body: string(body)}
	}

	var result struct {
		Payload []Contact `json:"payload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	// For groups, match by Identifier field or custom attribute gowa_whatsapp_jid
	// For private chats, match by phone number
	for _, contact := range result.Payload {
		if isIdentifierBased {
			if contact.Identifier == identifier {
				return &contact, nil
			}
			if jid, ok := contact.CustomAttributes["gowa_whatsapp_jid"].(string); ok && jid == identifier {
				return &contact, nil
			}
		} else {
			if contact.PhoneNumber == searchTerm {
				return &contact, nil
			}
		}
	}

	return nil, nil
}

func (c *Client) CreateContact(name, identifier string, isGroup bool) (*Contact, error) {
	endpoint := fmt.Sprintf("%s/api/v1/accounts/%d/contacts", c.BaseURL, c.AccountID)

	// For groups, use Identifier field
	// For private chats, use E.164 phone format
	// For @lid JIDs (non-phone WhatsApp IDs), use Identifier field
	var phoneNumber, contactIdentifier string
	if isGroup || strings.HasSuffix(identifier, "@lid") {
		contactIdentifier = identifier
	} else {
		phoneNumber = utils.NormalizePhoneE164(identifier)
	}

	payload := CreateContactRequest{
		InboxID:     c.InboxID,
		Name:        name,
		PhoneNumber: phoneNumber,
		Identifier:  contactIdentifier,
		CustomAttributes: map[string]any{
			"gowa_whatsapp_jid": identifier,
		},
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal contact payload: %w", err)
	}
	logrus.Debugf("Chatwoot CreateContact: Sending payload: %s", string(jsonPayload))
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api_access_token", c.APIToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	logrus.Debugf("Chatwoot CreateContact: Response status=%d body=%s", resp.StatusCode, string(bodyBytes))

	// Chatwoot returns 200 OK for contacts
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode, Op: "create contact", Body: string(bodyBytes)}
	}

	// Chatwoot API returns: {"payload": {"contact": {...}, "contact_inbox": {...}}}
	var nestedResult struct {
		Payload struct {
			Contact Contact `json:"contact"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(bodyBytes, &nestedResult); err == nil && nestedResult.Payload.Contact.ID != 0 {
		return &nestedResult.Payload.Contact, nil
	}

	// Fallback: some Chatwoot versions return {"payload": Contact{...}} directly
	var flatResult struct {
		Payload Contact `json:"payload"`
	}
	if err := json.Unmarshal(bodyBytes, &flatResult); err == nil && flatResult.Payload.ID != 0 {
		return &flatResult.Payload, nil
	}

	// Last resort: try direct decode (contact at root level)
	var contact Contact
	if err := json.Unmarshal(bodyBytes, &contact); err == nil && contact.ID != 0 {
		return &contact, nil
	}

	return nil, fmt.Errorf("failed to decode contact response (no valid ID found): %s", string(bodyBytes))
}

func (c *Client) FindOrCreateContact(name, identifier string, isGroup bool) (*Contact, error) {
	contact, err := c.FindContactByIdentifier(identifier, isGroup)
	if err != nil {
		return nil, err
	}
	if contact != nil {
		// Preserve manually edited 1:1 contact names; group subjects are
		// expected to keep following WhatsApp-side name changes. Blank 1:1
		// names still get the initial fallback for readability.
		shouldUpdateName := name != "" && contact.Name != name && (isGroup || strings.TrimSpace(contact.Name) == "")
		if shouldUpdateName {
			logrus.Infof("Chatwoot: Updating contact name from '%s' to '%s'", contact.Name, name)
			if err := c.UpdateContactName(contact.ID, name); err != nil {
				logrus.Warnf("Chatwoot: Failed to update contact name: %v", err)
				// Continue anyway, the old name is still usable
			}
			contact.Name = name
		}
		return contact, nil
	}

	created, err := c.CreateContact(name, identifier, isGroup)
	if err != nil {
		// A concurrent creator (another process, the direct-DB importer, or a
		// Chatwoot agent) can win the race between our find and create, leaving
		// Chatwoot to reject the duplicate phone/identifier with 422. Re-find
		// once and reuse the now-existing contact instead of dropping the
		// message; fall through to the original error if it still can't be found.
		var httpErr *HTTPStatusError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusUnprocessableEntity {
			if found, ferr := c.FindContactByIdentifier(identifier, isGroup); ferr == nil && found != nil {
				return found, nil
			}
		}
		return nil, err
	}
	return created, nil
}

// UpdateContactName updates the name of an existing contact
func (c *Client) UpdateContactName(contactID int, name string) error {
	endpoint := fmt.Sprintf("%s/api/v1/accounts/%d/contacts/%d", c.BaseURL, c.AccountID, contactID)

	payload := map[string]any{
		"name": name,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal update payload: %w", err)
	}
	req, err := http.NewRequest("PUT", endpoint, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api_access_token", c.APIToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return &HTTPStatusError{StatusCode: resp.StatusCode, Op: "update contact", Body: string(body)}
	}

	return nil
}

type conversationListItem struct {
	ID      int    `json:"id"`
	InboxID int    `json:"inbox_id"`
	Status  string `json:"status"`
}

// listContactConversations fetches all conversations for a contact via the
// contact-specific endpoint. Shared by FindConversation (which wants the
// active one) and FindLatestConversation (which wants the most recent one to
// reopen).
func (c *Client) listContactConversations(contactID int) ([]conversationListItem, error) {
	endpoint := fmt.Sprintf("%s/api/v1/accounts/%d/contacts/%d/conversations", c.BaseURL, c.AccountID, contactID)
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("api_access_token", c.APIToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode, Op: "list contact conversations", Body: string(body)}
	}

	var result struct {
		Payload []conversationListItem `json:"payload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Payload, nil
}

// selectOpenConversation returns the first open (non-resolved) conversation in
// this client's inbox, or nil when none is open.
func selectOpenConversation(items []conversationListItem, inboxID, contactID int) *Conversation {
	for _, conv := range items {
		if conv.InboxID == inboxID && conv.Status != "resolved" {
			return &Conversation{ID: conv.ID, ContactID: contactID, InboxID: conv.InboxID, Status: conv.Status}
		}
	}
	return nil
}

// selectLatestConversation returns the most recent conversation (highest id) in
// this client's inbox regardless of status, or nil when the contact has none.
func selectLatestConversation(items []conversationListItem, inboxID, contactID int) *Conversation {
	var latest *Conversation
	for _, conv := range items {
		if conv.InboxID != inboxID {
			continue
		}
		if latest == nil || conv.ID > latest.ID {
			latest = &Conversation{ID: conv.ID, ContactID: contactID, InboxID: conv.InboxID, Status: conv.Status}
		}
	}
	return latest
}

func (c *Client) FindConversation(contactID int) (*Conversation, error) {
	items, err := c.listContactConversations(contactID)
	if err != nil {
		return nil, err
	}
	return selectOpenConversation(items, c.InboxID, contactID), nil
}

// FindLatestConversation returns the most recent conversation for the contact
// in this client's inbox, regardless of status. The reopen path uses it to
// resurrect a resolved conversation instead of opening a new thread. Returns
// (nil, nil) when the contact has no conversation in this inbox.
func (c *Client) FindLatestConversation(contactID int) (*Conversation, error) {
	items, err := c.listContactConversations(contactID)
	if err != nil {
		return nil, err
	}
	return selectLatestConversation(items, c.InboxID, contactID), nil
}

// ToggleConversationStatus sets a conversation's status (open/resolved/pending)
// via POST /conversations/{id}/toggle_status.
func (c *Client) ToggleConversationStatus(conversationID int, status string) error {
	endpoint := fmt.Sprintf("%s/api/v1/accounts/%d/conversations/%d/toggle_status", c.BaseURL, c.AccountID, conversationID)
	jsonPayload, err := json.Marshal(map[string]string{"status": status})
	if err != nil {
		return fmt.Errorf("failed to marshal toggle status payload: %w", err)
	}
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api_access_token", c.APIToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &HTTPStatusError{StatusCode: resp.StatusCode, Op: "toggle conversation status", Body: string(body)}
	}
	return nil
}

// conversationStatusForNew returns the status a newly created or reopened
// conversation should land in: "pending" routes it to the unassigned queue
// when ChatwootConversationPending is set, otherwise "open" puts it in the
// agent's active queue.
func conversationStatusForNew() string {
	if config.ChatwootConversationPending {
		return "pending"
	}
	return "open"
}

// CreateConversation opens a new conversation for the contact. sourceID is the
// WhatsApp chat JID; it is sent so Chatwoot keys the conversation's
// contact_inbox by that JID (API-channel inboxes accept any source_id),
// matching the direct-DB importer. That alignment is what lets the contact
// public endpoints (e.g. update_last_seen) resolve the conversation later. An
// empty sourceID is omitted, leaving Chatwoot to generate one.
func (c *Client) CreateConversation(contactID int, sourceID string) (*Conversation, error) {
	endpoint := fmt.Sprintf("%s/api/v1/accounts/%d/conversations", c.BaseURL, c.AccountID)

	payload := CreateConversationRequest{
		InboxID:   c.InboxID,
		ContactID: contactID,
		Status:    conversationStatusForNew(),
		SourceID:  sourceID,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal conversation payload: %w", err)
	}
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api_access_token", c.APIToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode, Op: "create conversation", Body: string(body)}
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	logrus.Debugf("Chatwoot CreateConversation: Response body=%s", string(bodyBytes))

	var result struct {
		Payload Conversation `json:"payload"`
	}
	if err := json.Unmarshal(bodyBytes, &result); err == nil && result.Payload.ID != 0 {
		return &result.Payload, nil
	}

	var conversation Conversation
	if err := json.Unmarshal(bodyBytes, &conversation); err == nil && conversation.ID != 0 {
		return &conversation, nil
	}

	return nil, fmt.Errorf("failed to decode conversation response (no valid ID found): %s", string(bodyBytes))
}

// FindOrCreateConversation resolves the conversation an inbound message should
// land in. sourceID is the WhatsApp chat JID, forwarded to CreateConversation
// when a fresh thread is opened. The contact's conversation list is fetched
// once and scanned in-process for both the open-conversation and reopen cases.
func (c *Client) FindOrCreateConversation(contactID int, sourceID string) (*Conversation, error) {
	items, err := c.listContactConversations(contactID)
	if err != nil {
		// A list failure is logged and we fall through to creation rather than
		// dropping the message.
		logrus.Errorf("Error finding conversation: %v", err)
	}

	if conv := selectOpenConversation(items, c.InboxID, contactID); conv != nil {
		return conv, nil
	}

	// No active conversation. When reopen is enabled, prefer reopening the
	// contact's most recent (resolved) conversation over spawning a new thread,
	// so a returning customer continues their existing conversation — matching
	// the direct-DB importer, which always reuses the latest conversation row.
	if config.ChatwootReopenConversation {
		if latest := selectLatestConversation(items, c.InboxID, contactID); latest != nil {
			if latest.Status == "resolved" {
				target := conversationStatusForNew()
				if terr := c.ToggleConversationStatus(latest.ID, target); terr != nil {
					logrus.Warnf("Chatwoot: failed to reopen conversation %d: %v", latest.ID, terr)
				} else {
					latest.Status = target
				}
			}
			return latest, nil
		}
	}

	return c.CreateConversation(contactID, sourceID)
}

// ListInboxes returns every inbox in the account. Auto-provisioning uses it to
// reuse an existing inbox of the configured name instead of creating a
// duplicate on each restart.
func (c *Client) ListInboxes() ([]Inbox, error) {
	endpoint := fmt.Sprintf("%s/api/v1/accounts/%d/inboxes", c.BaseURL, c.AccountID)
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("api_access_token", c.APIToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode, Op: "list inboxes", Body: string(body)}
	}

	var result struct {
		Payload []Inbox `json:"payload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Payload, nil
}

// CreateInbox provisions a new API-channel inbox with the given name and
// optional webhook URL, returning the created inbox.
func (c *Client) CreateInbox(name, webhookURL string) (*Inbox, error) {
	endpoint := fmt.Sprintf("%s/api/v1/accounts/%d/inboxes", c.BaseURL, c.AccountID)
	payload := CreateInboxRequest{
		Name: name,
		Channel: CreateInboxChannel{
			Type:       "api",
			WebhookURL: webhookURL,
		},
	}
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal inbox payload: %w", err)
	}
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api_access_token", c.APIToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode, Op: "create inbox", Body: string(bodyBytes)}
	}

	// Chatwoot returns the inbox at the JSON root for this endpoint; tolerate a
	// {"payload":{...}} wrapper too for forward-compatibility.
	var inbox Inbox
	if err := json.Unmarshal(bodyBytes, &inbox); err == nil && inbox.ID != 0 {
		return &inbox, nil
	}
	var wrapped struct {
		Payload Inbox `json:"payload"`
	}
	if err := json.Unmarshal(bodyBytes, &wrapped); err == nil && wrapped.Payload.ID != 0 {
		return &wrapped.Payload, nil
	}
	return nil, fmt.Errorf("failed to decode inbox response (no valid ID found): %s", string(bodyBytes))
}

// MessageOptions carries optional fields for CreateMessage. SourceID stamps
// the Chatwoot message with the WhatsApp "WAID:<id>" identifier - the same
// convention the direct-DB importer uses - which both strengthens echo dedup
// and gives later replies a stable thread anchor. ContentAttributes carries
// reply metadata such as in_reply_to_external_id. Private creates a Chatwoot
// private note, used for agent-visible send failures. All fields are optional;
// the zero value reproduces the previous behavior exactly.
type MessageOptions struct {
	SourceID          string
	ContentAttributes map[string]any
	Private           bool
}

func (c *Client) CreateMessage(conversationID int, content string, messageType string, attachments []string, opt MessageOptions) (int, error) {
	endpoint := fmt.Sprintf("%s/api/v1/accounts/%d/conversations/%d/messages", c.BaseURL, c.AccountID, conversationID)

	if len(attachments) > 0 {
		return c.createMessageWithAttachments(endpoint, content, messageType, attachments, opt)
	}

	payload := CreateMessageRequest{
		Content:           content,
		MessageType:       messageType,
		Private:           opt.Private,
		SourceID:          opt.SourceID,
		ContentAttributes: opt.ContentAttributes,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal message payload: %w", err)
	}
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return 0, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api_access_token", c.APIToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return 0, &HTTPStatusError{StatusCode: resp.StatusCode, Op: "create message", Body: string(bodyBytes)}
	}

	var result struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(bodyBytes, &result); err == nil && result.ID != 0 {
		return result.ID, nil
	}

	return 0, nil
}

func (c *Client) DeleteMessage(conversationID, messageID int) error {
	endpoint := fmt.Sprintf("%s/api/v1/accounts/%d/conversations/%d/messages/%d", c.BaseURL, c.AccountID, conversationID, messageID)
	req, err := http.NewRequest(http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("api_access_token", c.APIToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return &HTTPStatusError{StatusCode: resp.StatusCode, Op: "delete message", Body: string(body)}
	}
	return nil
}

func (c *Client) UpdateLastSeen(conversationID int, contactInboxSourceID string) error {
	// inbox_identifier is immutable, so resolve it once and reuse the cached
	// value — read sync fires this per receipt and a full inbox-list GET each
	// time is pure overhead.
	inboxIdentifier := c.InboxIdentifier
	if inboxIdentifier == "" {
		inboxes, err := c.ListInboxes()
		if err != nil {
			return err
		}
		for _, inbox := range inboxes {
			if inbox.ID == c.InboxID {
				inboxIdentifier = inbox.InboxIdentifier
				break
			}
		}
		if inboxIdentifier == "" {
			return fmt.Errorf("chatwoot inbox %d has no inbox_identifier", c.InboxID)
		}
		c.InboxIdentifier = inboxIdentifier
	}

	endpoint := fmt.Sprintf(
		"%s/public/api/v1/inboxes/%s/contacts/%s/conversations/%d/update_last_seen",
		c.BaseURL,
		url.PathEscape(inboxIdentifier),
		url.PathEscape(contactInboxSourceID),
		conversationID,
	)
	req, err := http.NewRequest(http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("api_access_token", c.APIToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		return &HTTPStatusError{StatusCode: resp.StatusCode, Op: "update last seen", Body: string(body)}
	}
	return nil
}

func (c *Client) createMessageWithAttachments(endpoint, content, messageType string, attachments []string, opt MessageOptions) (int, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	if err := writer.WriteField("content", content); err != nil {
		return 0, fmt.Errorf("write content field: %w", err)
	}
	if err := writer.WriteField("message_type", messageType); err != nil {
		return 0, fmt.Errorf("write message_type field: %w", err)
	}
	if opt.Private {
		if err := writer.WriteField("private", "true"); err != nil {
			return 0, fmt.Errorf("write private field: %w", err)
		}
	} else {
		if err := writer.WriteField("private", "false"); err != nil {
			return 0, fmt.Errorf("write private field: %w", err)
		}
	}
	if opt.SourceID != "" {
		if err := writer.WriteField("source_id", opt.SourceID); err != nil {
			return 0, fmt.Errorf("write source_id field: %w", err)
		}
	}
	if len(opt.ContentAttributes) > 0 {
		attrs, err := json.Marshal(opt.ContentAttributes)
		if err != nil {
			return 0, fmt.Errorf("marshal content attributes: %w", err)
		}
		if err := writer.WriteField("content_attributes", string(attrs)); err != nil {
			return 0, fmt.Errorf("write content_attributes field: %w", err)
		}
	}

	for _, filePath := range attachments {
		file, err := os.Open(filePath)
		if err != nil {
			return 0, fmt.Errorf("open attachment %s: %w", filePath, err)
		}

		fileName := filepath.Base(filePath)
		ext := filepath.Ext(filePath)

		mimeType := mime.TypeByExtension(ext)
		if mimeType == "" {
			if ext == ".oga" {
				mimeType = "audio/ogg"
			} else {
				mimeType = "application/octet-stream"
			}
		}

		// Custom form part with correct Content-Type for Chatwoot to render images inline.
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="attachments[]"; filename="%s"`, fileName))
		h.Set("Content-Type", mimeType)

		part, err := writer.CreatePart(h)
		if err != nil {
			file.Close()
			return 0, fmt.Errorf("create attachment part %s: %w", filePath, err)
		}
		if _, err := io.Copy(part, file); err != nil {
			file.Close()
			return 0, fmt.Errorf("copy attachment %s: %w", filePath, err)
		}
		if err := file.Close(); err != nil {
			return 0, fmt.Errorf("close attachment %s: %w", filePath, err)
		}
	}

	if err := writer.Close(); err != nil {
		return 0, fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequest("POST", endpoint, body)
	if err != nil {
		return 0, err
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("api_access_token", c.APIToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return 0, &HTTPStatusError{StatusCode: resp.StatusCode, Op: "create message with attachments", Body: string(respBody)}
	}

	var result struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(respBody, &result); err == nil && result.ID != 0 {
		return result.ID, nil
	}

	return 0, nil
}
