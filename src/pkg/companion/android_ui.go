package companion

import (
	"context"
	"encoding/xml"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Node struct {
	Attrs    map[string]string
	Parent   *Node
	Children []*Node
}

func (n *Node) get(k string) string {
	if n == nil {
		return ""
	}
	return n.Attrs[k]
}

type Screen struct {
	Nodes []*Node
	Words string
}

func ParseScreen(source, packageName string) (*Screen, error) {
	if len(source) > 2_000_000 {
		return nil, block("unknown_screen")
	}
	d := xml.NewDecoder(strings.NewReader(source))
	var stack []*Node
	s := &Screen{}
	hasPackage := false
	for {
		token, e := d.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, block("unknown_screen")
		}
		switch t := token.(type) {
		case xml.StartElement:
			n := &Node{Attrs: map[string]string{}}
			for _, a := range t.Attr {
				n.Attrs[a.Name.Local] = a.Value
			}
			if len(stack) > 0 {
				n.Parent = stack[len(stack)-1]
				n.Parent.Children = append(n.Parent.Children, n)
			}
			stack = append(stack, n)
			if t.Name.Local == "node" {
				s.Nodes = append(s.Nodes, n)
				s.Words += strings.ToLower(n.get("text")+" "+n.get("content-desc")) + "\n"
				hasPackage = hasPackage || n.get("package") == packageName
			}
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, block("unknown_screen")
			}
			stack = stack[:len(stack)-1]
		}
	}
	if strings.Contains(s.Words, "your account is restricted") || strings.Contains(s.Words, "temporarily banned") {
		delay := 21600
		re := regexp.MustCompile(`\b(\d{1,2}):(\d{2}):(\d{2})\b`)
		m := re.FindStringSubmatch(s.Words)
		if m != nil {
			h, _ := strconv.Atoi(m[1])
			minutes, _ := strconv.Atoi(m[2])
			seconds, _ := strconv.Atoi(m[3])
			delay = h*3600 + minutes*60 + seconds + 300
		}
		return nil, &Blocked{"restriction", delay}
	}
	for _, word := range []string{"verify your phone number", "enter your pin", "verify your identity", "fingerprint", "registration code", "verification code", "6-digit code", "one-time password", "two-step verification", "confirm your phone number", "enter your password"} {
		if strings.Contains(s.Words, word) {
			return nil, block("owner_verification")
		}
	}
	if !hasPackage || len(stack) != 0 {
		return nil, block("unknown_screen")
	}
	return s, nil
}
func (s *Screen) Matching(label string) []*Node {
	var nodes []*Node
	for _, n := range s.Nodes {
		desc := n.get("content-desc")
		if strings.EqualFold(strings.TrimSpace(n.get("text")), label) || strings.EqualFold(strings.TrimSpace(desc), label) || strings.EqualFold(strings.TrimSpace(strings.Split(desc, ",")[0]), label) {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

var boundsPattern = regexp.MustCompile(`^\[(\d+),(\d+)\]\[(\d+),(\d+)\]$`)

func (s *Screen) Point(n *Node) (int, int, error) {
	original := n
	menu := false
	for n != nil && n.get("clickable") != "true" {
		menu = menu || strings.HasSuffix(n.get("class"), "ListView")
		n = n.Parent
	}
	if n == nil && menu && original.get("enabled") == "true" {
		n = original
	}
	if n == nil || n.get("enabled") != "true" {
		return 0, 0, block("unknown_screen")
	}
	m := boundsPattern.FindStringSubmatch(n.get("bounds"))
	if m == nil {
		return 0, 0, block("unknown_screen")
	}
	v := []int{}
	for _, s := range m[1:] {
		i, e := strconv.Atoi(s)
		if e != nil || i > 100000 {
			return 0, 0, block("unknown_screen")
		}
		v = append(v, i)
	}
	if v[0] >= v[2] || v[1] >= v[3] {
		return 0, 0, block("unknown_screen")
	}
	return (v[0] + v[2]) / 2, (v[1] + v[3]) / 2, nil
}
func (s *Screen) Unique(label string) (int, int, error) {
	nodes := s.Matching(label)
	if len(nodes) == 0 {
		return 0, 0, block("unknown_screen")
	}
	x, y, e := s.Point(nodes[0])
	if e != nil {
		return 0, 0, e
	}
	for _, n := range nodes[1:] {
		a, b, e := s.Point(n)
		if e != nil || a != x || b != y {
			return 0, 0, block("unknown_screen")
		}
	}
	return x, y, nil
}
func (s *Screen) Title(label string) bool {
	for _, n := range s.Nodes {
		if strings.EqualFold(strings.TrimSpace(n.get("text")), label) && strings.HasSuffix(n.get("class"), "TextView") {
			return true
		}
		if n.get("selected") == "true" && strings.EqualFold(strings.TrimSpace(strings.Split(n.get("content-desc"), ",")[0]), label) {
			return true
		}
	}
	return false
}
func (s *Screen) IsCode() bool {
	return (strings.Contains(s.Words, "enter code") || strings.Contains(s.Words, "enter the code")) && len(s.fields()) > 0
}
func (s *Screen) fields() []*Node {
	var fields []*Node
	for _, n := range s.Nodes {
		if strings.HasSuffix(n.get("class"), "EditText") {
			fields = append(fields, n)
		}
	}
	return fields
}

// UITransport must capture private PNG and XML evidence before each UI action.
// The transport must keep its whole-session reservation through Restore/Release.
type UITransport interface {
	Start(context.Context) error
	Capture(context.Context) (string, error)
	Tap(context.Context, int, int) error
	Back(context.Context) error
	Swipe(context.Context) error
	Text(context.Context, string) error
	Restore(context.Context, string) error
	Release() error
}
type PairingUI struct {
	Transport                                   UITransport
	Account                                     Account
	Package                                     string
	identityVerified, started, prepared         bool
	origin                                      string
	CharacterPause, PollInterval, SubmitTimeout time.Duration
}

func (u *PairingUI) capture(ctx context.Context) (*Screen, error) {
	xml, e := u.Transport.Capture(ctx)
	if e != nil {
		return nil, e
	}
	return ParseScreen(xml, u.Package)
}
func (u *PairingUI) click(ctx context.Context, s *Screen, label string) (*Screen, error) {
	x, y, e := s.Unique(label)
	if e != nil {
		return nil, e
	}
	if e = u.Transport.Tap(ctx, x, y); e != nil {
		return nil, e
	}
	return u.capture(ctx)
}
func (u *PairingUI) settings(ctx context.Context, s *Screen) (*Screen, error) {
	for range 5 {
		if s.Title("Settings") && len(s.Matching("Linked devices")) > 0 {
			return s, nil
		}
		if len(s.Matching("More options")) > 0 {
			menu, e := u.click(ctx, s, "More options")
			if e != nil {
				return nil, e
			}
			if len(menu.Matching("Settings")) > 0 {
				return u.click(ctx, menu, "Settings")
			}
			if e = u.Transport.Back(ctx); e != nil {
				return nil, e
			}
		}
		if e := u.Transport.Back(ctx); e != nil {
			return nil, e
		}
		var e error
		s, e = u.capture(ctx)
		if e != nil {
			return nil, e
		}
	}
	return nil, block("unknown_screen")
}
func (u *PairingUI) Begin(ctx context.Context) (Object, error) {
	u.started = true
	if e := u.Transport.Start(ctx); e != nil {
		return nil, e
	}
	s, e := u.capture(ctx)
	if e != nil {
		return nil, e
	}
	if len(s.fields()) > 0 {
		return nil, block("phone_busy")
	}
	for _, name := range []string{"Updates", "Chats", "Settings", "Linked devices"} {
		if s.Title(name) {
			u.origin = name
			break
		}
	}
	if u.origin == "" {
		return nil, block("unknown_screen")
	}
	settings, e := u.settings(ctx, s)
	if e != nil {
		return nil, e
	}
	labels := settings.Matching(u.Account.DisplayName)
	if len(labels) != 1 {
		return nil, block("identity_mismatch")
	}
	x, y, e := settings.Point(labels[0])
	if e != nil {
		return nil, e
	}
	if e = u.Transport.Tap(ctx, x, y); e != nil {
		return nil, e
	}
	profile, e := u.capture(ctx)
	if e != nil {
		return nil, e
	}
	digits := regexp.MustCompile(`[^0-9]`)
	for range 4 {
		for _, n := range profile.Nodes {
			value := n.get("text") + " " + n.get("content-desc")
			if strings.Contains(value, "+") && digits.ReplaceAllString(value, "") == u.Account.Phone {
				u.identityVerified = true
				break
			}
		}
		if u.identityVerified {
			break
		}
		if !profile.Title("Edit profile") && !profile.Title("Profile") {
			return nil, block("identity_mismatch")
		}
		if e = u.Transport.Swipe(ctx); e != nil {
			return nil, e
		}
		profile, e = u.capture(ctx)
		if e != nil {
			return nil, e
		}
	}
	if !u.identityVerified {
		return nil, block("identity_mismatch")
	}
	if e = u.Transport.Back(ctx); e != nil {
		return nil, e
	}
	s, e = u.capture(ctx)
	if e != nil {
		return nil, e
	}
	s, e = u.settings(ctx, s)
	if e != nil {
		return nil, e
	}
	if _, e = u.click(ctx, s, "Linked devices"); e != nil {
		return nil, e
	}
	return Object{"identity_verified": true}, nil
}

var gowaEntry = regexp.MustCompile(`^Google Chrome \(GOWA v[^)]+\)$`)
var capacity = regexp.MustCompile(`\b(\d+) of (\d+) devices linked`)

func loggedOut(n *Node) bool {
	if strings.EqualFold(n.get("text"), "logged out") {
		return true
	}
	for _, c := range n.Children {
		if loggedOut(c) {
			return true
		}
	}
	return false
}
func (u *PairingUI) Prepare(ctx context.Context) (Object, error) {
	if !u.identityVerified {
		return nil, block("identity_mismatch")
	}
	s, e := u.capture(ctx)
	if e != nil {
		return nil, e
	}
	if !s.Title("Linked devices") {
		s, e = u.settings(ctx, s)
		if e != nil {
			return nil, e
		}
		s, e = u.click(ctx, s, "Linked devices")
		if e != nil {
			return nil, e
		}
	}
	entries := []*Node{}
	for _, n := range s.Nodes {
		if !gowaEntry.MatchString(n.get("text")) {
			continue
		}
		row := n
		for row != nil && row.get("clickable") != "true" {
			row = row.Parent
		}
		if row == nil {
			row = n
		}
		if loggedOut(row) {
			entries = append(entries, n)
		}
	}
	if len(entries) > 1 {
		return nil, block("ambiguous_device")
	}
	if len(entries) == 1 {
		x, y, e := s.Point(entries[0])
		if e != nil {
			return nil, e
		}
		if e = u.Transport.Tap(ctx, x, y); e != nil {
			return nil, e
		}
		s, e = u.capture(ctx)
		if e != nil {
			return nil, e
		}
		s, e = u.click(ctx, s, "LOG IN")
	} else {
		m := capacity.FindStringSubmatch(s.Words)
		if m != nil {
			used, _ := strconv.Atoi(m[1])
			limit, _ := strconv.Atoi(m[2])
			if used >= limit {
				return nil, block("slots_full")
			}
		}
		s, e = u.click(ctx, s, "LINK A DEVICE")
	}
	if e != nil {
		return nil, e
	}
	if len(s.Matching("Link with phone number instead")) > 0 {
		s, e = u.click(ctx, s, "Link with phone number instead")
	} else if len(s.Matching("Link with phone number")) > 0 {
		s, e = u.click(ctx, s, "Link with phone number")
	}
	if e != nil {
		return nil, e
	}
	if !s.IsCode() {
		return nil, block("unknown_screen")
	}
	u.prepared = true
	return Object{"ready": true}, nil
}
func pause(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return block("adapter_timeout")
	case <-time.After(d):
		return nil
	}
}

// namingField recognizes only the post-link device-label form, not an arbitrary
// text prompt. Existing labels, extra fields and owner verification fail closed.
func (u *PairingUI) namingField(s *Screen, expected string) (*Node, error) {
	if !s.Title("Device name") || !strings.Contains(s.Words, "this helps you assign and manage devices.") || len(s.Matching("SAVE")) != 1 {
		return nil, block("unknown_screen")
	}
	fields := s.fields()
	if len(fields) != 1 || fields[0].get("package") != u.Package || fields[0].get("enabled") != "true" || fields[0].get("text") != expected {
		return nil, block("unknown_screen")
	}
	return fields[0], nil
}

func (u *PairingUI) nameDevice(ctx context.Context, s *Screen, delay time.Duration) (*Screen, error) {
	field, e := u.namingField(s, "")
	if e != nil {
		return nil, e
	}
	if field.get("focused") != "true" {
		x, y, e := s.Point(field)
		if e != nil {
			return nil, e
		}
		if e = u.Transport.Tap(ctx, x, y); e != nil {
			return nil, e
		}
		s, e = u.capture(ctx)
		if e != nil {
			return nil, e
		}
		field, e = u.namingField(s, "")
		if e != nil {
			return nil, e
		}
	}
	if field.get("focused") != "true" {
		return nil, block("unknown_screen")
	}
	// A generic app label, entered through the same character-only private pipe.
	// Do not overwrite a pre-existing name or copy account/host names into it.
	const label = "GOWA"
	for _, c := range label {
		if e = u.Transport.Text(ctx, string(c)); e != nil {
			return nil, e
		}
		if e = pause(ctx, delay); e != nil {
			return nil, e
		}
	}
	s, e = u.capture(ctx)
	if e != nil {
		return nil, e
	}
	if _, e = u.namingField(s, label); e != nil {
		return nil, e
	}
	return u.click(ctx, s, "SAVE")
}

func (u *PairingUI) Enter(ctx context.Context, code string) (Object, error) {
	if !u.prepared || !pairingCode.MatchString(code) {
		return nil, block("invalid_request")
	}
	s, e := u.capture(ctx)
	if e != nil {
		return nil, e
	}
	if !s.IsCode() {
		return nil, block("unknown_screen")
	}
	focused := false
	for _, n := range s.fields() {
		focused = focused || n.get("focused") == "true"
	}
	if !focused {
		return nil, block("unknown_screen")
	}
	delay := u.CharacterPause
	if delay == 0 {
		delay = 400 * time.Millisecond
	}
	// Clear prepared before typing: a partial entry must never be silently repeated.
	u.prepared = false
	for _, c := range strings.ReplaceAll(code, "-", "") {
		if e = u.Transport.Text(ctx, string(c)); e != nil {
			return nil, e
		}
		if e = pause(ctx, delay); e != nil {
			return nil, e
		}
	}
	timeout := u.SubmitTimeout
	if timeout == 0 {
		timeout = 90 * time.Second
	}
	poll := u.PollInterval
	if poll == 0 {
		poll = 2 * time.Second
	}
	deadline := time.Now().Add(timeout)
	namingHandled := false
	for time.Now().Before(deadline) {
		s, e = u.capture(ctx)
		if e != nil {
			return nil, e
		}
		if s.Title("Device name") {
			if namingHandled {
				return nil, block("pairing_unconfirmed")
			}
			namingHandled = true
			s, e = u.nameDevice(ctx, s, delay)
			if e != nil {
				return nil, e
			}
		}
		if s.Title("Linked devices") {
			return Object{"submitted": true}, nil
		}
		if s.IsCode() && (strings.Contains(s.Words, "incorrect code") || strings.Contains(s.Words, "try again")) {
			return nil, block("pairing_unconfirmed")
		}
		if e = pause(ctx, poll); e != nil {
			return nil, e
		}
	}
	return nil, block("pairing_unconfirmed")
}
func (u *PairingUI) Finish(ctx context.Context) error {
	if !u.started {
		return nil
	}
	u.started = false
	e := u.Transport.Restore(ctx, u.origin)
	releaseErr := u.Transport.Release()
	if e != nil {
		return e
	}
	return releaseErr
}

type PhoneWorker struct {
	UI           *PairingUI
	NewTransport func(Object, Account) (UITransport, error)
}

func (w *PhoneWorker) Call(ctx context.Context, op string, r Object) (Object, error) {
	if op == "begin" && w.UI == nil {
		raw, e := jsonObject(r["account"])
		if e != nil {
			return nil, e
		}
		a := Account{ID: stringValue(raw["id"]), Phone: stringValue(raw["phone"]), DisplayName: stringValue(raw["display_name"])}
		if !identifier.MatchString(a.ID) || !phoneNumber.MatchString(a.Phone) || a.DisplayName == "" {
			return nil, block("invalid_request")
		}
		config, e := jsonObject(r["config"])
		if e != nil {
			return nil, e
		}
		t, e := w.NewTransport(config, a)
		if e != nil {
			return nil, e
		}
		w.UI = &PairingUI{Transport: t, Account: a, Package: stringValue(config["package"])}
		if w.UI.Package == "" {
			w.UI.Package = "com.whatsapp.w4b"
		}
		return w.UI.Begin(ctx)
	}
	if op == "finish" {
		if w.UI != nil {
			if e := w.UI.Finish(ctx); e != nil {
				return nil, e
			}
		}
		return Object{}, nil
	}
	if w.UI == nil {
		return nil, block("invalid_request")
	}
	switch op {
	case "prepare_pair":
		return w.UI.Prepare(ctx)
	case "enter_code":
		return w.UI.Enter(ctx, stringValue(r["code"]))
	}
	return nil, block("invalid_request")
}
func (w *PhoneWorker) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if w.UI != nil {
		return w.UI.Finish(ctx)
	}
	return nil
}
func stringValue(v any) string { s, _ := v.(string); return s }
func jsonObject(v any) (Object, error) {
	if v == nil {
		return Object{}, nil
	}
	o, ok := v.(map[string]any)
	if !ok {
		return nil, block("invalid_request")
	}
	return o, nil
}
