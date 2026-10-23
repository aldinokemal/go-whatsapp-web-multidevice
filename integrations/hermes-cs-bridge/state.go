package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Pause marks a chat where the AI must stay silent (admin took over).
type Pause struct {
	Until  time.Time `json:"until"`
	Reason string    `json:"reason"`
}

// Store keeps the state that must survive a restart: paused chats and the
// phone-number → chat-id map used by admin commands.
type Store struct {
	mu     sync.Mutex
	path   string
	Paused map[string]Pause  `json:"paused"`
	Phones map[string]string `json:"phones"`
}

func LoadStore(path string) (*Store, error) {
	s := &Store{path: path, Paused: map[string]Pause{}, Phones: map[string]string{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, err
	}
	if s.Paused == nil {
		s.Paused = map[string]Pause{}
	}
	if s.Phones == nil {
		s.Phones = map[string]string{}
	}
	return s, nil
}

// save writes atomically; the caller holds s.mu.
func (s *Store) save() error {
	if s.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) PausedUntil(chatID string, now time.Time) (Pause, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.Paused[chatID]
	if !ok {
		return Pause{}, false
	}
	if !now.Before(p.Until) {
		delete(s.Paused, chatID)
		_ = s.save()
		return Pause{}, false
	}
	return p, true
}

func (s *Store) SetPause(chatID string, until time.Time, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Paused[chatID] = Pause{Until: until, Reason: reason}
	return s.save()
}

func (s *Store) ClearPause(chatID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Paused, chatID)
	return s.save()
}

func (s *Store) RememberPhone(phone, chatID string) {
	if phone == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Phones[phone] == chatID {
		return
	}
	s.Phones[phone] = chatID
	_ = s.save()
}

// ChatForPhone returns the chat id last seen for a phone, or the plain
// phone JID when the number never wrote to the CS number.
func (s *Store) ChatForPhone(phone string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.Phones[phone]; ok {
		return c
	}
	return phone + "@s.whatsapp.net"
}

func (s *Store) ActivePauses(now time.Time) map[string]Pause {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]Pause{}
	for k, p := range s.Paused {
		if now.Before(p.Until) {
			out[k] = p
		}
	}
	return out
}
