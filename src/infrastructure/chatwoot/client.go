package chatwoot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
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
}

var (
	defaultClient     *Client
	defaultClientOnce sync.Once
)

// GetDefaultClient returns a shared Chatwoot client instance.
// Uses lazy initialization with sync.Once for thread safety.
func GetDefaultClient() *Client {
	defaultClientOnce.Do(func() {
		defaultClient = NewClient()
	})
	return defaultClient
}

func NewClient() *Client {
	return &Client{
		BaseURL:   strings.TrimRight(config.ChatwootURL, "/"),
		APIToken:  config.ChatwootAPIToken,
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

// doRequest executes an HTTP request with common headers and error handling.
// It marshals the payload to JSON (if provided), sets auth headers, executes the request,
// and decodes the response into result (if provided).
func (c *Client) doRequest(method, endpoint string, payload interface{}, result interface{}) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		jsonPayload, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal payload: %w", err)
		}
		body = bytes.NewBuffer(jsonPayload)
	}

	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		return nil, err
	}

	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("api_access_token", c.APIToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return bodyBytes, fmt.Errorf("request failed: status %d body %s", resp.StatusCode, string(bodyBytes))
	}

	if result != nil && len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, result); err != nil {
			return bodyBytes, fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return bodyBytes, nil
}

func (c *Client) FindContactByIdentifier(identifier string, isGroup bool) (*Contact, error) {
	endpoint := fmt.Sprintf("%s/api/v1/accounts/%d/contacts/search", c.BaseURL, c.AccountID)
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	// For groups, search by the identifier directly
	// For private chats, add + prefix for E.164 format
	searchTerm := identifier
	if !isGroup {
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
		return nil, fmt.Errorf("failed to search contact: status %d", resp.StatusCode)
	}

	var result struct {
		Payload []Contact `json:"payload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	// For groups, match by Identifier field or custom attribute waha_whatsapp_jid
	// For private chats, match by phone number
	for _, contact := range result.Payload {
		if isGroup {
			// Check Identifier field first
			if contact.Identifier == identifier {
				return &contact, nil
			}
			// Fallback: check custom attributes for group JID
			if jid, ok := contact.CustomAttributes["waha_whatsapp_jid"].(string); ok && jid == identifier {
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
	var phoneNumber, contactIdentifier string
	if isGroup {
		// Use the group JID as the identifier
		contactIdentifier = identifier
	} else {
		phoneNumber = utils.NormalizePhoneE164(identifier)
	}

	payload := CreateContactRequest{
		InboxID:     c.InboxID,
		Name:        name,
		PhoneNumber: phoneNumber,
		Identifier:  contactIdentifier,
		CustomAttributes: map[string]interface{}{
			"waha_whatsapp_jid": identifier,
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
		return nil, fmt.Errorf("failed to create contact: status %d body %s", resp.StatusCode, string(bodyBytes))
	}

	// Try to decode with payload wrapper first
	var result struct {
		Payload Contact `json:"payload"`
	}
	if err := json.Unmarshal(bodyBytes, &result); err == nil && result.Payload.ID != 0 {
		return &result.Payload, nil
	}

	// Try direct decode (some Chatwoot versions return contact directly)
	var contact Contact
	if err := json.Unmarshal(bodyBytes, &contact); err != nil {
		return nil, fmt.Errorf("failed to decode contact response: %v", err)
	}

	return &contact, nil
}

func (c *Client) FindOrCreateContact(name, identifier string, isGroup bool) (*Contact, error) {
	contact, err := c.FindContactByIdentifier(identifier, isGroup)
	if err != nil {
		return nil, err
	}
	if contact != nil {
		// Update contact name if it has changed (e.g., group name changed)
		if contact.Name != name && name != "" {
			logrus.Infof("Chatwoot: Updating contact name from '%s' to '%s'", contact.Name, name)
			if err := c.UpdateContactName(contact.ID, name); err != nil {
				logrus.Warnf("Chatwoot: Failed to update contact name: %v", err)
				// Continue anyway, the old name is still usable
			}
			contact.Name = name
		}
		return contact, nil
	}
	return c.CreateContact(name, identifier, isGroup)
}

// UpdateContactName updates the name of an existing contact
func (c *Client) UpdateContactName(contactID int, name string) error {
	endpoint := fmt.Sprintf("%s/api/v1/accounts/%d/contacts/%d", c.BaseURL, c.AccountID, contactID)

	payload := map[string]interface{}{
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
		return fmt.Errorf("failed to update contact: status %d body %s", resp.StatusCode, string(body))
	}

	return nil
}

func (c *Client) FindConversation(contactID int) (*Conversation, error) {
	// Use contact-specific conversations endpoint for accurate results
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
		return nil, fmt.Errorf("failed to list contact conversations: status %d body %s", resp.StatusCode, string(body))
	}

	var result struct {
		Payload []struct {
			ID      int    `json:"id"`
			InboxID int    `json:"inbox_id"`
			Status  string `json:"status"`
		} `json:"payload"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	// Find an open conversation for this inbox
	for _, conv := range result.Payload {
		if conv.InboxID == c.InboxID && conv.Status != "resolved" {
			return &Conversation{
				ID:        conv.ID,
				ContactID: contactID,
				InboxID:   conv.InboxID,
				Status:    conv.Status,
			}, nil
		}
	}

	return nil, nil
}

func (c *Client) CreateConversation(contactID int) (*Conversation, error) {
	endpoint := fmt.Sprintf("%s/api/v1/accounts/%d/conversations", c.BaseURL, c.AccountID)

	payload := CreateConversationRequest{
		InboxID:   c.InboxID,
		ContactID: contactID,
		Status:    "open",
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
		return nil, fmt.Errorf("failed to create conversation: status %d body %s", resp.StatusCode, string(body))
	}

	var result struct {
		Payload Conversation `json:"payload"`
	}
	// Try to decode into wrapper first
	bodyBytes, _ := io.ReadAll(resp.Body)
	// Reset body for decoder
	resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	if err := json.NewDecoder(resp.Body).Decode(&result); err == nil && result.Payload.ID != 0 {
		return &result.Payload, nil
	}

	// If wrapper failed, try direct decode
	resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	var conversation Conversation
	if err := json.NewDecoder(resp.Body).Decode(&conversation); err != nil {
		return nil, err
	}
	return &conversation, nil
}

func (c *Client) FindOrCreateConversation(contactID int) (*Conversation, error) {
	conv, err := c.FindConversation(contactID)
	if err != nil {
		logrus.Errorf("Error finding conversation: %v", err)
	}
	if conv != nil {
		return conv, nil
	}
	return c.CreateConversation(contactID)
}

func (c *Client) CreateMessage(conversationID int, content string, messageType string, attachments []string) error {
	endpoint := fmt.Sprintf("%s/api/v1/accounts/%d/conversations/%d/messages", c.BaseURL, c.AccountID, conversationID)

	if len(attachments) > 0 {
		return c.createMessageWithAttachments(endpoint, content, messageType, attachments)
	}

	payload := CreateMessageRequest{
		Content:     content,
		MessageType: messageType,
		Private:     false,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal message payload: %w", err)
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
		return fmt.Errorf("failed to create message: status %d body %s", resp.StatusCode, string(body))
	}

	return nil
}

func (c *Client) createMessageWithAttachments(endpoint, content, messageType string, attachments []string) error {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	_ = writer.WriteField("content", content)
	_ = writer.WriteField("message_type", messageType)
	_ = writer.WriteField("private", "false")

	for _, filePath := range attachments {
		// Process each file in a closure to ensure proper cleanup of file handles
		// This prevents file descriptor leaks when processing multiple attachments
		func(fp string) {
			file, err := os.Open(fp)
			if err != nil {
				logrus.Errorf("Failed to open file %s: %v", fp, err)
				return
			}
			defer file.Close()

			// Get file name from path
			fileName := filepath.Base(fp)

			// Detect MIME type from file extension
			mimeType := mime.TypeByExtension(filepath.Ext(fp))
			if mimeType == "" {
				mimeType = "application/octet-stream"
			}

			// Create a custom form part with correct Content-Type header
			// This is needed for Chatwoot to render images inline instead of as file attachments
			h := make(textproto.MIMEHeader)
			h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="attachments[]"; filename="%s"`, fileName))
			h.Set("Content-Type", mimeType)

			part, err := writer.CreatePart(h)
			if err != nil {
				logrus.Errorf("Failed to create form part for %s: %v", fp, err)
				return
			}
			io.Copy(part, file)
		}(filePath)
	}

	writer.Close()

	req, err := http.NewRequest("POST", endpoint, body)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("api_access_token", c.APIToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to create message with attachments: status %d body %s", resp.StatusCode, string(respBody))
	}

	return nil
}
