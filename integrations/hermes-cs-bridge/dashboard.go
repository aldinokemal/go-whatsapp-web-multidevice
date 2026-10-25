package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Lead events for the Ruang Sanad Dashboard CRM (POST /integrations/cs-ai/events).
// Contract: docs/prd/prd-crm-leads-cs-ai.md (section 7.1, appendix B) in the
// dashboard repo. Events are queued in a local file first, so a Dashboard
// outage or restart of the bridge loses nothing; the Dashboard ignores
// repeated event ids.

const (
	dashboardIngestPath = "/integrations/cs-ai/events"
	dashboardBatchMax   = 100
	dashboardBatchBytes = 200 << 10 // the Dashboard accepts at most 256 KB per request
	dashboardQueueMax   = 50000
	dashboardBodyMax    = 4000 // runes of a message body sent to the Dashboard
	dashboardTick       = 2 * time.Second
	dashboardBackoffMax = 10 * time.Minute
)

// DashboardEvent is one lead event as the Dashboard expects it.
type DashboardEvent struct {
	EventID    string         `json:"event_id"`
	Type       string         `json:"type"`
	OccurredAt string         `json:"occurred_at"`
	ChatID     string         `json:"chat_id"`
	Phone      string         `json:"phone,omitempty"`
	WaName     string         `json:"wa_name,omitempty"`
	Data       map[string]any `json:"data"`
}

// EventSink receives lead events. A nil sink means the feature is off.
type EventSink interface {
	Emit(ev DashboardEvent)
}

// DashboardOutbox persists events to a JSONL file and sends them in batches.
type DashboardOutbox struct {
	url, token string
	file       string
	http       *http.Client
	log        *slog.Logger

	mu    sync.Mutex
	queue []DashboardEvent
	wake  chan struct{}
}

// NewDashboardOutbox loads events left from a previous run. It returns nil
// (feature off) when the Dashboard URL or token is empty.
func NewDashboardOutbox(cfg Config, log *slog.Logger) (*DashboardOutbox, error) {
	if cfg.DashboardURL == "" || cfg.DashboardToken == "" {
		return nil, nil
	}
	o := &DashboardOutbox{
		url: cfg.DashboardURL + dashboardIngestPath, token: cfg.DashboardToken,
		file: filepath.Join(cfg.DashboardOutboxDir, "queue.jsonl"),
		http: &http.Client{Timeout: 15 * time.Second}, log: log, wake: make(chan struct{}, 1),
	}
	if err := os.MkdirAll(cfg.DashboardOutboxDir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.Open(o.file)
	if errors.Is(err, os.ErrNotExist) {
		return o, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var ev DashboardEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			log.Warn("dashboard outbox: skipped unreadable line", "err", err)
			continue
		}
		o.queue = append(o.queue, ev)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(o.queue) > 0 {
		log.Info("dashboard outbox: resuming queued events", "count", len(o.queue))
	}
	return o, nil
}

// Emit queues an event. It never blocks on the network.
func (o *DashboardOutbox) Emit(ev DashboardEvent) {
	if ev.EventID == "" {
		ev.EventID = newEventID()
	}
	if ev.OccurredAt == "" {
		ev.OccurredAt = time.Now().UTC().Format(time.RFC3339)
	}
	if ev.Data == nil {
		ev.Data = map[string]any{}
	}
	o.mu.Lock()
	o.queue = append(o.queue, ev)
	if over := len(o.queue) - dashboardQueueMax; over > 0 {
		o.log.Warn("dashboard outbox full; dropping oldest events", "dropped", over)
		o.queue = append([]DashboardEvent(nil), o.queue[over:]...)
		o.saveLocked()
	} else {
		o.appendLocked(ev)
	}
	o.mu.Unlock()
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

func (o *DashboardOutbox) appendLocked(ev DashboardEvent) {
	line, err := json.Marshal(ev)
	if err != nil {
		return
	}
	f, err := os.OpenFile(o.file, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		o.log.Error("dashboard outbox: cannot write queue file", "err", err)
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

// saveLocked rewrites the queue file atomically; the caller holds o.mu.
func (o *DashboardOutbox) saveLocked() {
	var buf bytes.Buffer
	for _, ev := range o.queue {
		line, err := json.Marshal(ev)
		if err != nil {
			continue
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	tmp := o.file + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		o.log.Error("dashboard outbox: cannot write queue file", "err", err)
		return
	}
	if err := os.Rename(tmp, o.file); err != nil {
		o.log.Error("dashboard outbox: cannot replace queue file", "err", err)
	}
}

// Pending returns the number of queued events (for /healthz and tests).
func (o *DashboardOutbox) Pending() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.queue)
}

// Run sends queued events until ctx ends, backing off after failures.
func (o *DashboardOutbox) Run(ctx context.Context) {
	backoff := time.Duration(0)
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-o.wake:
			if backoff > 0 { // still waiting after a failure
				continue
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		for {
			sent, err := o.flushOnce(ctx)
			if err != nil {
				backoff = min(max(backoff*2, dashboardTick), dashboardBackoffMax)
				o.log.Warn("dashboard outbox: send failed, will retry", "err", err, "retry_in", backoff, "pending", o.Pending())
				break
			}
			backoff = 0
			if sent == 0 || ctx.Err() != nil {
				break
			}
		}
		next := dashboardTick
		if backoff > 0 {
			next = backoff
		}
		timer.Reset(next)
	}
}

// nextBatch takes up to 100 events from the head of the queue, kept under the
// request size limit.
func (o *DashboardOutbox) nextBatch() ([]DashboardEvent, []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()
	var batch []DashboardEvent
	size := 16
	for _, ev := range o.queue {
		if len(batch) >= dashboardBatchMax {
			break
		}
		line, _ := json.Marshal(ev)
		if len(batch) > 0 && size+len(line)+1 > dashboardBatchBytes {
			break
		}
		batch = append(batch, ev)
		size += len(line) + 1
	}
	if len(batch) == 0 {
		return nil, nil
	}
	body, _ := json.Marshal(map[string]any{"events": batch})
	return batch, body
}

// ack removes the batch just handled from the queue (matched by id, because
// the queue may have dropped its oldest events in the meantime).
func (o *DashboardOutbox) ack(batch []DashboardEvent) {
	done := make(map[string]bool, len(batch))
	for _, ev := range batch {
		done[ev.EventID] = true
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	kept := make([]DashboardEvent, 0, len(o.queue))
	for _, ev := range o.queue {
		if !done[ev.EventID] {
			kept = append(kept, ev)
		}
	}
	o.queue = kept
	o.saveLocked()
}

// flushOnce sends one batch. It returns how many events left the queue.
func (o *DashboardOutbox) flushOnce(ctx context.Context) (int, error) {
	batch, body := o.nextBatch()
	if len(batch) == 0 {
		return 0, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+o.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))

	switch {
	case resp.StatusCode == http.StatusOK:
		var r struct {
			Accepted   int `json:"accepted"`
			Duplicates int `json:"duplicates"`
			Rejected   []struct {
				EventID string `json:"event_id"`
				Error   string `json:"error"`
			} `json:"rejected"`
		}
		_ = json.Unmarshal(out, &r)
		for _, rj := range r.Rejected {
			// Rejected for its schema: resending would fail again, so drop it.
			o.log.Error("dashboard rejected event", "event_id", rj.EventID, "err", rj.Error)
		}
		o.ack(batch)
		return len(batch), nil

	case resp.StatusCode == http.StatusBadRequest || (resp.StatusCode == http.StatusRequestEntityTooLarge && len(batch) == 1):
		// The whole request can never succeed as it is; drop it instead of
		// blocking every later event.
		o.log.Error("dashboard refused batch; dropping it", "status", resp.StatusCode, "events", len(batch), "body", truncate(string(out), 300))
		o.ack(batch)
		return len(batch), nil

	case resp.StatusCode == http.StatusNotFound:
		return 0, errors.New("dashboard ingest is off (HTTP 404): set CS_BRIDGE_INGEST_TOKEN in the Dashboard .env")
	case resp.StatusCode == http.StatusUnauthorized:
		return 0, errors.New("dashboard refused the token (HTTP 401): DASHBOARD_INGEST_TOKEN must equal CS_BRIDGE_INGEST_TOKEN")
	case resp.StatusCode == http.StatusForbidden:
		return 0, errors.New("dashboard refused this address (HTTP 403): call it directly (not through Nginx) from an address in CS_BRIDGE_ALLOWED_CIDRS")
	default:
		return 0, fmt.Errorf("dashboard: HTTP %d: %s", resp.StatusCode, truncate(string(out), 200))
	}
}

func newEventID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("evt_%d_%s", time.Now().UnixNano(), hex.EncodeToString(b[:]))
}

// ---- emit helpers used by the bridge ---------------------------------------

// leadRef identifies the lead chat an event belongs to.
type leadRef struct {
	chatID, phone, name string
	skipCRM             bool // not a lead (e.g. a chat with an admin number)
}

func (b *Bridge) emit(ref leadRef, typ, eventID string, at time.Time, data map[string]any) {
	if b.events == nil || ref.chatID == "" || ref.skipCRM {
		return
	}
	phone := ref.phone
	if phone == "" && strings.HasSuffix(ref.chatID, "@s.whatsapp.net") {
		phone = normalizePhone(ref.chatID)
	}
	if body, ok := data["body"].(string); ok {
		data["body"] = truncate(body, dashboardBodyMax)
	}
	b.events.Emit(DashboardEvent{
		EventID: eventID, Type: typ, OccurredAt: at.UTC().Format(time.RFC3339),
		ChatID: ref.chatID, Phone: phone, WaName: ref.name, Data: data,
	})
}

// leadFor returns what the bridge knows about a chat (name and phone from the
// last incoming message).
func (b *Bridge) leadFor(chatID string) leadRef {
	b.mu.Lock()
	defer b.mu.Unlock()
	ref := leadRef{chatID: chatID}
	if c, ok := b.chats[chatID]; ok {
		ref.phone, ref.name = c.phone, c.name
	}
	return ref
}

// messageTime is the WhatsApp timestamp of a webhook message, or now.
func messageTime(p map[string]any, now time.Time) time.Time {
	if t, err := time.Parse(time.RFC3339, str(p, "timestamp")); err == nil {
		return t
	}
	return now
}
