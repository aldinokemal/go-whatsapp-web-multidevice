// Package passkey wraps go-webauthn for WTF: discoverable (usernameless) passkeys.
//
// The service is HTTP-agnostic. The web layer keeps the ceremony id it gets
// from Begin* in an HttpOnly cookie (wtf_pk) and hands it back to Finish*.
// Registration must only be reachable with a signed-in session and a fresh
// CSRF token; login is unauthenticated and must sit behind RequireSameOrigin
// and a rate limit. Nothing here logs: challenges, credential data and
// ceremony ids never leave this package except through the return values.
package passkey

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

var (
	// ErrCeremony means the ceremony id is unknown, expired, already used,
	// belongs to another user or to the other flow (register vs login).
	ErrCeremony = errors.New("passkey request expired; please try again")
	// ErrInvalid means the authenticator's answer did not verify. Its text
	// never carries the challenge, origin or credential data.
	ErrInvalid = errors.New("passkey not recognised")
	// ErrDisabled means the account the passkey belongs to is disabled.
	ErrDisabled = errors.New("this account is disabled")
	// ErrBusy means too many ceremonies are pending (flood protection).
	ErrBusy = errors.New("too many passkey requests; please try again shortly")
	// ErrTooMany means the user already has the maximum number of passkeys.
	ErrTooMany = errors.New("you already have the maximum number of passkeys")
)

const (
	ceremonyTTL     = 5 * time.Minute
	defaultMax      = 10000 // pending ceremonies across all users
	sweepEvery      = time.Minute
	maxBodyBytes    = 64 << 10
	maxNameRunes    = 64
	maxPasskeys     = 10
	defaultName     = "Passkey"
	kindRegister    = "register"
	kindLogin       = "login"
	auditAdd        = "passkey_add"
	auditCloneAlarm = "passkey_clone_warning"
)

// verifyError keeps only the go-webauthn error *type* (a fixed identifier
// such as "verification_error"), never its details or debug info, which can
// hold the challenge and origins.
type verifyError struct{ kind string }

func (e verifyError) Error() string        { return ErrInvalid.Error() + " (" + e.kind + ")" }
func (e verifyError) Is(target error) bool { return target == ErrInvalid }

func invalid(err error) error {
	var pe *protocol.Error
	if errors.As(err, &pe) && pe.Type != "" {
		return verifyError{kind: pe.Type}
	}
	return verifyError{kind: "invalid"}
}

type ceremony struct {
	data    webauthn.SessionData
	kind    string
	userID  string // empty for login
	expires time.Time
}

type Service struct {
	wa        *webauthn.WebAuthn
	st        *store.Store
	mu        sync.Mutex
	cer       map[string]ceremony
	regOf     map[string]string // user id -> their pending registration ceremony id
	lastSweep time.Time
	max       int
	now       func() time.Time
}

// FromPublicURL derives the RP ID and origin from the configured public URL
// (never from request headers).
func FromPublicURL(publicURL string) (rpID, origin string, err error) {
	u, err := parseOrigin(strings.TrimRight(strings.TrimSpace(publicURL), "/"))
	if err != nil {
		return "", "", err
	}
	return u.Hostname(), u.Scheme + "://" + u.Host, nil
}

func parseOrigin(origin string) (*url.URL, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return nil, errors.New("passkey: origin must be an absolute URL")
	}
	if u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("passkey: origin must be scheme://host[:port] only")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && u.Hostname() == "localhost":
	default:
		return nil, errors.New("passkey: origin must be https (http only for localhost)")
	}
	return u, nil
}

func New(rpID, origin, displayName string, st *store.Store) (*Service, error) {
	rpID = strings.ToLower(strings.TrimSpace(rpID))
	u, err := parseOrigin(strings.TrimSpace(origin))
	if err != nil {
		return nil, err
	}
	if host := u.Hostname(); rpID == "" || (host != rpID && !strings.HasSuffix(host, "."+rpID)) {
		return nil, errors.New("passkey: origin is not within the RP ID")
	}
	timeout := webauthn.TimeoutConfig{Enforce: true, Timeout: ceremonyTTL, TimeoutUVD: ceremonyTTL}
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: displayName,
		RPOrigins:     []string{u.Scheme + "://" + u.Host},
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationRequired,
		},
		Timeouts: webauthn.TimeoutsConfig{Login: timeout, Registration: timeout},
	})
	if err != nil {
		return nil, err
	}
	return &Service{
		wa: wa, st: st, cer: map[string]ceremony{}, regOf: map[string]string{},
		max: defaultMax, now: time.Now,
	}, nil
}

type waUser struct {
	u     *store.User
	creds []webauthn.Credential
}

func (w waUser) WebAuthnID() []byte                         { return []byte(w.u.ID) }
func (w waUser) WebAuthnName() string                       { return w.u.Phone }
func (w waUser) WebAuthnDisplayName() string                { return w.u.Phone }
func (w waUser) WebAuthnCredentials() []webauthn.Credential { return w.creds }

func (s *Service) load(ctx context.Context, u *store.User) (waUser, error) {
	rows, err := s.st.PasskeysOf(ctx, u.ID)
	if err != nil {
		return waUser{}, err
	}
	w := waUser{u: u}
	for _, r := range rows {
		var c webauthn.Credential
		if err := json.Unmarshal(r.CredentialJSON, &c); err == nil {
			w.creds = append(w.creds, c)
		}
	}
	return w, nil
}

// activeUser re-reads the user so a status change since the session was
// loaded is honoured.
func (s *Service) activeUser(ctx context.Context, id string) (*store.User, error) {
	u, err := s.st.UserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if u.Status != store.StatusActive {
		return nil, ErrDisabled
	}
	return u, nil
}

// sweepLocked drops expired ceremonies. It runs at most once a minute unless
// the store is full, so a flood cannot make every Begin an O(n) scan.
func (s *Service) sweepLocked(now time.Time) {
	if len(s.cer) < s.max && now.Sub(s.lastSweep) < sweepEvery {
		return
	}
	s.lastSweep = now
	for k, v := range s.cer {
		if now.After(v.expires) {
			s.dropLocked(k, v)
		}
	}
}

func (s *Service) dropLocked(id string, c ceremony) {
	delete(s.cer, id)
	if c.kind == kindRegister && s.regOf[c.userID] == id {
		delete(s.regOf, c.userID)
	}
}

func (s *Service) put(data *webauthn.SessionData, kind, userID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if kind == kindRegister {
		if old, ok := s.regOf[userID]; ok {
			s.dropLocked(old, s.cer[old])
		}
	}
	s.sweepLocked(now)
	if len(s.cer) >= s.max {
		return "", ErrBusy
	}
	id := store.NewToken()
	s.cer[id] = ceremony{data: *data, kind: kind, userID: userID, expires: now.Add(ceremonyTTL)}
	if kind == kindRegister {
		s.regOf[userID] = id
	}
	return id, nil
}

// take removes the ceremony whatever happens next: one attempt per ceremony.
func (s *Service) take(id, kind, userID string) (webauthn.SessionData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cer[id]
	if !ok {
		return webauthn.SessionData{}, ErrCeremony
	}
	s.dropLocked(id, c)
	if s.now().After(c.expires) || c.kind != kind || c.userID != userID {
		return webauthn.SessionData{}, ErrCeremony
	}
	return c.data, nil
}

func (s *Service) pending() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.cer)
}

func credKey(id []byte) string { return base64.RawURLEncoding.EncodeToString(id) }

func cleanName(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if r := []rune(name); len(r) > maxNameRunes {
		name = strings.TrimSpace(string(r[:maxNameRunes]))
	}
	if name == "" {
		return defaultName
	}
	return name
}

// BeginRegistration starts adding a passkey for the signed-in user u. A new
// call replaces u's previous pending registration.
func (s *Service) BeginRegistration(ctx context.Context, u *store.User) ([]byte, string, error) {
	u, err := s.activeUser(ctx, u.ID)
	if err != nil {
		return nil, "", err
	}
	w, err := s.load(ctx, u)
	if err != nil {
		return nil, "", err
	}
	if len(w.creds) >= maxPasskeys {
		return nil, "", ErrTooMany
	}
	exclude := make([]protocol.CredentialDescriptor, 0, len(w.creds))
	for _, c := range w.creds {
		exclude = append(exclude, c.Descriptor())
	}
	opts, data, err := s.wa.BeginRegistration(w,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithExclusions(exclude))
	if err != nil {
		return nil, "", err
	}
	js, err := json.Marshal(opts)
	if err != nil {
		return nil, "", err
	}
	id, err := s.put(data, kindRegister, u.ID)
	if err != nil {
		return nil, "", err
	}
	return js, id, nil
}

// FinishRegistration verifies the authenticator's answer and stores the passkey.
func (s *Service) FinishRegistration(ctx context.Context, u *store.User, ceremonyID string, body []byte, name string) error {
	data, err := s.take(ceremonyID, kindRegister, u.ID)
	if err != nil {
		return err
	}
	if len(body) > maxBodyBytes {
		return verifyError{kind: "too_large"}
	}
	u, err = s.activeUser(ctx, u.ID)
	if err != nil {
		return err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(body)
	if err != nil {
		return invalid(err)
	}
	w, err := s.load(ctx, u)
	if err != nil {
		return err
	}
	if len(w.creds) >= maxPasskeys {
		return ErrTooMany
	}
	cred, err := s.wa.CreateCredential(w, data, parsed)
	if err != nil {
		return invalid(err)
	}
	key := credKey(cred.ID)
	if _, err := s.st.PasskeyByCredentialID(ctx, key); err == nil {
		return verifyError{kind: "already_registered"}
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	js, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	if err := s.st.AddPasskey(ctx, store.Passkey{CredentialID: key, UserID: u.ID, Name: cleanName(name), CredentialJSON: js}); err != nil {
		return err
	}
	_ = s.st.Audit(ctx, store.AuditEvent{UserID: u.ID, Action: auditAdd})
	return nil
}

// BeginLogin starts a usernameless sign-in.
func (s *Service) BeginLogin(ctx context.Context) ([]byte, string, error) {
	opts, data, err := s.wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, "", err
	}
	js, err := json.Marshal(opts)
	if err != nil {
		return nil, "", err
	}
	id, err := s.put(data, kindLogin, "")
	if err != nil {
		return nil, "", err
	}
	return js, id, nil
}

// FinishLogin verifies a usernameless sign-in and returns the user it proves.
// The user is resolved from the credential id, which must belong to the user
// named by the userHandle; disabled users are refused.
func (s *Service) FinishLogin(ctx context.Context, ceremonyID string, body []byte) (*store.User, error) {
	data, err := s.take(ceremonyID, kindLogin, "")
	if err != nil {
		return nil, err
	}
	if len(body) > maxBodyBytes {
		return nil, verifyError{kind: "too_large"}
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(body)
	if err != nil {
		return nil, invalid(err)
	}
	var found *store.User
	var lookupErr error
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		pk, err := s.st.PasskeyByCredentialID(ctx, credKey(rawID))
		if err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				lookupErr = err
			}
			return nil, errors.New("unknown passkey")
		}
		if !bytes.Equal([]byte(pk.UserID), userHandle) {
			return nil, errors.New("passkey does not belong to that user")
		}
		u, err := s.st.UserByID(ctx, pk.UserID)
		if err != nil {
			lookupErr = err
			return nil, errors.New("unknown user")
		}
		w, err := s.load(ctx, u)
		if err != nil {
			lookupErr = err
			return nil, errors.New("unknown user")
		}
		found = u
		return w, nil
	}
	_, cred, err := s.wa.ValidatePasskeyLogin(handler, data, parsed)
	if lookupErr != nil {
		return nil, fmt.Errorf("passkey lookup: %w", lookupErr)
	}
	if err != nil {
		return nil, invalid(err)
	}
	if found == nil || cred == nil || !bytes.Equal(cred.ID, parsed.RawID) {
		return nil, verifyError{kind: "invalid"}
	}
	if cred.Authenticator.CloneWarning {
		// The counter went backwards: a copy of this key may exist. Refuse
		// and keep the stored (higher) counter.
		_ = s.st.Audit(ctx, store.AuditEvent{UserID: found.ID, Action: auditCloneAlarm})
		return nil, verifyError{kind: "clone_warning"}
	}
	// Status is checked only after the signature verified, so the answer
	// "disabled" goes only to someone holding the key.
	if found.Status != store.StatusActive {
		return nil, ErrDisabled
	}
	js, err := json.Marshal(cred)
	if err != nil {
		return nil, err
	}
	if err := s.st.UpdatePasskeyCredential(ctx, credKey(cred.ID), js); err != nil {
		return nil, err
	}
	return found, nil
}
