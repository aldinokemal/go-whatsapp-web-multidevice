// Package upload is a small, in-memory, S3-presigned-URL-shaped ticket
// store: mint a ticket (an unguessable random id, short-lived, bound to one
// upload), PUT bytes to it once, GET them back until it expires. It exists
// so an MCP client holding only local bytes - a screenshot it just took,
// with nothing to host them at - can still reach image_url, which only ever
// downloads from a URL: no persistent secret changes hands, only this one
// short-lived, single-purpose ticket.
package upload

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	ErrNotFound        = errors.New("upload ticket not found")
	ErrExpired         = errors.New("upload ticket expired")
	ErrAlreadyUploaded = errors.New("upload ticket already used")
	ErrTooLarge        = errors.New("upload exceeds the size limit")
	ErrUnsupportedType = errors.New("unsupported content type")
	ErrTooManyTickets  = errors.New("too many outstanding upload tickets")
)

// maxOutstandingTicketsPerDevice is the real limit a legitimate caller ever
// hits: one agent working through one image at a time needs one or two, not
// twenty. maxOutstandingTickets is a much higher backstop on the whole
// store, so one device spamming create_upload cannot starve every other
// device sharing this instance (the per-device cap alone would not stop a
// caller who mints tickets under many different device ids).
const (
	maxOutstandingTicketsPerDevice = 20
	maxOutstandingTickets          = 1000
)

// AllowedMimes is the content-type allowlist every upload is sniffed
// against, matching the image types whatsapp_send already accepts.
var AllowedMimes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

type ticket struct {
	deviceID  string
	expiresAt time.Time
	maxBytes  int64
	uploaded  bool
	filePath  string
	mimeType  string
}

// Store is safe for concurrent use.
type Store struct {
	mu      sync.Mutex
	tickets map[string]*ticket
	dir     string
}

// NewStore keeps uploaded files under dir, which must already exist.
func NewStore(dir string) *Store {
	return &Store{tickets: make(map[string]*ticket), dir: dir}
}

// ClearDirectory deletes every file directly under dir. Tickets live only
// in memory, so a restart orphans whatever the previous process had
// uploaded with no ticket left to ever expire it; call this once, before
// constructing the Store that will own dir, so a crash-restart loop cannot
// accumulate files forever.
func ClearDirectory(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue // this directory only ever holds upload files, but never recurse into a surprise one
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// Create mints a new ticket id, valid for ttl, accepting at most maxBytes.
func (s *Store) Create(deviceID string, ttl time.Duration, maxBytes int64) (string, error) {
	idBytes := make([]byte, 20) // 160 bits: unguessable is the only credential here
	if _, err := rand.Read(idBytes); err != nil {
		return "", err
	}
	id := hex.EncodeToString(idBytes)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Expire anything already due before counting against either cap, so a
	// backlog merely waiting for the next Sweep tick can't itself look like
	// exhausted quota.
	now := time.Now()
	deviceCount := 0
	for existingID, existing := range s.tickets {
		if now.After(existing.expiresAt) {
			s.expireLocked(existingID, existing)
			continue
		}
		if existing.deviceID == deviceID {
			deviceCount++
		}
	}
	if deviceCount >= maxOutstandingTicketsPerDevice || len(s.tickets) >= maxOutstandingTickets {
		return "", ErrTooManyTickets
	}
	s.tickets[id] = &ticket{
		deviceID:  deviceID,
		expiresAt: time.Now().Add(ttl),
		maxBytes:  maxBytes,
	}
	return id, nil
}

// Upload writes body for id, once. A second call for the same id, an
// expired or unknown id, an oversized body, or an unrecognised image type
// are all refused; the disk write happens without the lock held.
func (s *Store) Upload(id string, body []byte) error {
	s.mu.Lock()
	t, ok := s.tickets[id]
	if !ok {
		s.mu.Unlock()
		return ErrNotFound
	}
	if time.Now().After(t.expiresAt) {
		s.expireLocked(id, t)
		s.mu.Unlock()
		return ErrExpired
	}
	if t.uploaded {
		s.mu.Unlock()
		return ErrAlreadyUploaded
	}
	if int64(len(body)) > t.maxBytes {
		s.mu.Unlock()
		return ErrTooLarge
	}
	mimeType := http.DetectContentType(body)
	if !AllowedMimes[mimeType] {
		s.mu.Unlock()
		return ErrUnsupportedType
	}
	// Claim the slot before the disk write so a second PUT racing this one
	// is refused rather than overwriting the file mid-read by a GET.
	t.uploaded = true
	path := filepath.Join(s.dir, id+extensionFor(mimeType))
	s.mu.Unlock()

	if err := os.WriteFile(path, body, 0o644); err != nil {
		os.Remove(path) // a partial file from a failed write (e.g. disk full)
		s.mu.Lock()
		t.uploaded = false // release the claim; the caller may retry
		s.mu.Unlock()
		return err
	}

	s.mu.Lock()
	t.filePath = path
	t.mimeType = mimeType
	s.mu.Unlock()
	return nil
}

// Fetch returns the uploaded file's path and mime type for id. It never
// consumes the ticket: a GET may be retried (a real send, a flaky network)
// until the ticket's own expiry, exactly like a presigned GET URL.
func (s *Store) Fetch(id string) (path, mimeType string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tickets[id]
	if !ok {
		return "", "", ErrNotFound
	}
	if time.Now().After(t.expiresAt) {
		s.expireLocked(id, t)
		return "", "", ErrExpired
	}
	if !t.uploaded {
		return "", "", ErrNotFound
	}
	return t.filePath, t.mimeType, nil
}

// Sweep deletes every expired ticket and the file it owned. Call it
// periodically; it is cheap and safe to call often.
func (s *Store) Sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, t := range s.tickets {
		if now.After(t.expiresAt) {
			s.expireLocked(id, t)
		}
	}
}

// expireLocked removes id's ticket and the file it owned, if any. Callers
// must hold s.mu. Every path that can drop a ticket from the map because it
// has expired (Upload, Fetch, Sweep) goes through this, so an uploaded file
// is never left behind once its ticket is gone - the earlier shape deleted
// the map entry in Fetch/Upload without touching the file, which made Sweep
// (the only thing that ever removed a file) unable to find it again: a
// permanently orphaned file, not merely a late one.
func (s *Store) expireLocked(id string, t *ticket) {
	if t.filePath != "" {
		os.Remove(t.filePath)
	}
	delete(s.tickets, id)
}

func extensionFor(mimeType string) string {
	switch mimeType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	default:
		return ".bin"
	}
}
