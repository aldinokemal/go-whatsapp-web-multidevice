package chatwoot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFindOrCreateContact_PreservesExistingIndividualName(t *testing.T) {
	tests := []struct {
		name         string
		incomingName string
	}{
		{
			name:         "incoming WhatsApp name differs",
			incomingName: "Alice WA",
		},
		{
			name:         "incoming name is phone fallback",
			incomingName: "6281234567890",
		},
		{
			name:         "incoming name is empty",
			incomingName: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			putCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts/1/contacts/search":
					if got := r.URL.Query().Get("q"); got != "+6281234567890" {
						t.Fatalf("search q = %q, want +6281234567890", got)
					}
					writeJSON(t, w, http.StatusOK, map[string]any{
						"payload": []Contact{{
							ID:          123,
							Name:        "Manual Alice",
							PhoneNumber: "+6281234567890",
						}},
					})
				case r.Method == http.MethodPut && r.URL.Path == "/api/v1/accounts/1/contacts/123":
					putCalls++
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
				}
			}))
			defer server.Close()

			client := &Client{
				BaseURL:    server.URL,
				APIToken:   "token",
				AccountID:  1,
				InboxID:    2,
				HTTPClient: server.Client(),
			}

			contact, err := client.FindOrCreateContact(tc.incomingName, "6281234567890", false)
			if err != nil {
				t.Fatalf("FindOrCreateContact: %v", err)
			}
			if contact.Name != "Manual Alice" {
				t.Fatalf("contact name = %q, want Manual Alice", contact.Name)
			}
			if putCalls != 0 {
				t.Fatalf("PUT contact calls = %d, want 0", putCalls)
			}
		})
	}
}

func TestFindOrCreateContact_FillsBlankExistingIndividualName(t *testing.T) {
	putCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts/1/contacts/search":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"payload": []Contact{{
					ID:          123,
					Name:        "",
					PhoneNumber: "+6281234567890",
				}},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/accounts/1/contacts/123":
			putCalls++
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode PUT body: %v", err)
			}
			if body["name"] != "6281234567890" {
				t.Fatalf("PUT name = %q, want 6281234567890", body["name"])
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := &Client{
		BaseURL:    server.URL,
		APIToken:   "token",
		AccountID:  1,
		InboxID:    2,
		HTTPClient: server.Client(),
	}

	contact, err := client.FindOrCreateContact("6281234567890", "6281234567890", false)
	if err != nil {
		t.Fatalf("FindOrCreateContact: %v", err)
	}
	if contact.Name != "6281234567890" {
		t.Fatalf("contact name = %q, want 6281234567890", contact.Name)
	}
	if putCalls != 1 {
		t.Fatalf("PUT contact calls = %d, want 1", putCalls)
	}
}

func TestFindOrCreateContact_RefreshesExistingGroupName(t *testing.T) {
	const groupJID = "120363123456789@g.us"
	putCalls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts/1/contacts/search":
			if got := r.URL.Query().Get("q"); got != groupJID {
				t.Fatalf("search q = %q, want %s", got, groupJID)
			}
			writeJSON(t, w, http.StatusOK, map[string]any{
				"payload": []Contact{{
					ID:         456,
					Name:       "Old Group",
					Identifier: groupJID,
				}},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/accounts/1/contacts/456":
			putCalls++
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode PUT body: %v", err)
			}
			if body["name"] != "New Group" {
				t.Fatalf("PUT name = %q, want New Group", body["name"])
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := &Client{
		BaseURL:    server.URL,
		APIToken:   "token",
		AccountID:  1,
		InboxID:    2,
		HTTPClient: server.Client(),
	}

	contact, err := client.FindOrCreateContact("New Group", groupJID, true)
	if err != nil {
		t.Fatalf("FindOrCreateContact: %v", err)
	}
	if contact.Name != "New Group" {
		t.Fatalf("contact name = %q, want New Group", contact.Name)
	}
	if putCalls != 1 {
		t.Fatalf("PUT contact calls = %d, want 1", putCalls)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatalf("write JSON: %v", err)
	}
}
