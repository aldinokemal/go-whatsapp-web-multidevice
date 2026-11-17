package chatwoot

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// authHeaderCases lists every function in this package that builds an HTTP
// request to Chatwoot, keyed by function name, with a call that makes it send
// at least one request. TestEveryRequestBuilderIsCovered fails when a function
// that builds a request is missing from this table, so a new call cannot ship
// without its authentication header being asserted.
var authHeaderCases = map[string]func(t *testing.T, c *Client){
	"FindContactByIdentifier": func(t *testing.T, c *Client) {
		_, _ = c.FindContactByIdentifier("628123456789@s.whatsapp.net", false)
	},
	"CreateContact": func(t *testing.T, c *Client) {
		_, _ = c.CreateContact("Name", "628123456789@s.whatsapp.net", false)
	},
	"UpdateContactName": func(t *testing.T, c *Client) {
		_ = c.UpdateContactName(7, "Name")
	},
	"listContactConversations": func(t *testing.T, c *Client) {
		_, _ = c.listContactConversations(7)
	},
	"ToggleConversationStatus": func(t *testing.T, c *Client) {
		_ = c.ToggleConversationStatus(5, "open")
	},
	"CreateConversation": func(t *testing.T, c *Client) {
		_, _ = c.CreateConversation(7, "628123456789@s.whatsapp.net")
	},
	"ListInboxes": func(t *testing.T, c *Client) {
		_, _ = c.ListInboxes()
	},
	"CreateInbox": func(t *testing.T, c *Client) {
		_, _ = c.CreateInbox("WhatsApp", "https://gowa.example.com/chatwoot/webhook")
	},
	"CreateMessage": func(t *testing.T, c *Client) {
		_, _ = c.CreateMessage(5, "hello", "incoming", nil, MessageOptions{})
	},
	"DeleteMessage": func(t *testing.T, c *Client) {
		_ = c.DeleteMessage(5, 9)
	},
	"UpdateLastSeen": func(t *testing.T, c *Client) {
		c.InboxIdentifier = "api-inbox-token"
		_ = c.UpdateLastSeen(5, "628123456789@s.whatsapp.net")
	},
	"createMessageWithAttachments": func(t *testing.T, c *Client) {
		tmp := filepath.Join(t.TempDir(), "note.txt")
		if err := os.WriteFile(tmp, []byte("data"), 0o600); err != nil {
			t.Fatalf("write temp file: %v", err)
		}
		_, _ = c.CreateMessage(5, "hello", "incoming", []string{tmp}, MessageOptions{})
	},
}

// TestEveryRequestCarriesTheAPITokenHeader drives each request-building
// function against a recording server and requires every request it sends to
// carry the token in the hyphenated header, and not in the underscore spelling
// that nginx discards by default.
func TestEveryRequestCarriesTheAPITokenHeader(t *testing.T) {
	for name, call := range authHeaderCases {
		t.Run(name, func(t *testing.T) {
			var (
				mu       sync.Mutex
				requests int
				problems []string
			)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				requests++
				where := r.Method + " " + r.URL.Path
				if got := r.Header.Values("Api-Access-Token"); len(got) != 1 || got[0] != "secret-token" {
					problems = append(problems, where+": api-access-token = "+strings.Join(got, ",")+", want exactly secret-token")
				}
				for key := range r.Header {
					if strings.Contains(key, "_") {
						problems = append(problems, where+": header with an underscore: "+key)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()

			c := newTestClient(t, server.URL)
			c.APIToken = "secret-token"
			call(t, c)

			mu.Lock()
			defer mu.Unlock()
			if requests == 0 {
				t.Fatalf("%s sent no request, so its header was not checked", name)
			}
			for _, p := range problems {
				t.Error(p)
			}
		})
	}
}

// TestEveryRequestBuilderIsCovered parses the package source and requires each
// function that builds an HTTP request to appear in authHeaderCases, and
// forbids the underscore spelling of the header name in non-test code.
func TestEveryRequestBuilderIsCovered(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	fset := token.NewFileSet()
	var builders []string
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			buildsRequest := false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.SelectorExpr:
					if pkg, ok := node.X.(*ast.Ident); ok && pkg.Name == "http" && strings.HasPrefix(node.Sel.Name, "NewRequest") {
						buildsRequest = true
					}
				case *ast.BasicLit:
					if node.Kind == token.STRING && strings.EqualFold(strings.Trim(node.Value, "\"`"), "api_access_token") {
						t.Errorf("%s: %s uses the header name api_access_token; use apiTokenHeader", file, fn.Name.Name)
					}
				}
				return true
			})
			if buildsRequest {
				builders = append(builders, fn.Name.Name)
			}
		}
	}
	if len(builders) == 0 {
		t.Fatal("found no request-building function; the source scan is broken")
	}
	sort.Strings(builders)
	seen := map[string]bool{}
	for _, name := range builders {
		seen[name] = true
		if _, ok := authHeaderCases[name]; !ok {
			t.Errorf("%s builds an HTTP request but has no entry in authHeaderCases", name)
		}
	}
	for name := range authHeaderCases {
		if !seen[name] {
			t.Errorf("authHeaderCases lists %s, which no longer builds an HTTP request", name)
		}
	}
}
