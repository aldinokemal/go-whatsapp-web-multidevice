package companion

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type ADBConfig struct {
	Serial   string `json:"serial"`
	User     *int   `json:"user"`
	Package  string `json:"package"`
	ADB      string `json:"adb"`
	StateDir string `json:"state_dir"`
}
type displayState struct {
	screenOn, locked bool
	user             int
	packageName      string
}
type ADB struct {
	config              ADBConfig
	directory, evidence string
	lock                *fileLock
	original            *displayState
	awakened            bool
	width, height       int
	execute             func(context.Context, []string, []byte) ([]byte, error)
}

func NewADB(config Object, _ Account) (UITransport, error) {
	raw, e := json.Marshal(config)
	if e != nil {
		return nil, block("invalid_request")
	}
	var c ADBConfig
	if json.Unmarshal(raw, &c) != nil || !regexp.MustCompile(`^[a-zA-Z0-9_-]+$`).MatchString(c.Serial) || c.User == nil || *c.User < 0 || c.StateDir == "" {
		return nil, block("invalid_request")
	}
	if c.Package == "" {
		c.Package = "com.whatsapp.w4b"
	}
	if c.Package != "com.whatsapp" && c.Package != "com.whatsapp.w4b" {
		return nil, block("invalid_request")
	}
	if c.ADB == "" {
		c.ADB = "adb"
	}
	if e = privateDir(c.StateDir); e != nil {
		return nil, e
	}
	path := filepath.Join(c.StateDir, randomID())
	if e = os.Mkdir(path, 0700); e != nil {
		return nil, e
	}
	return &ADB{config: c, directory: c.StateDir, evidence: path}, nil
}
func randomID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic("random source unavailable")
	}
	return hex.EncodeToString(b[:])
}
func (a *ADB) run(ctx context.Context, args []string, data []byte) ([]byte, error) {
	if a.execute != nil {
		return a.execute(ctx, args, data)
	}
	c, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	argv := append([]string{"-s", a.config.Serial}, args...)
	cmd := exec.CommandContext(c, a.config.ADB, argv...)
	cmd.Stdin = bytes.NewReader(data)
	var out bytes.Buffer
	cmd.Stdout = &out
	if cmd.Run() != nil {
		return nil, block("phone_unavailable")
	}
	return out.Bytes(), nil
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func (a *ADB) shell(ctx context.Context, args ...string) (string, error) {
	quoted := make([]string, len(args))
	for i, s := range args {
		quoted[i] = shellQuote(s)
	}
	raw, e := a.run(ctx, []string{"shell", "-T"}, []byte(strings.Join(quoted, " ")+"\n"))
	return string(raw), e
}

var keyguardPattern = regexp.MustCompile(`KeyguardServiceDelegate[\s\S]*?\n\s+showing=(true|false)`)
var foregroundPattern = regexp.MustCompile(`(?:topResumedActivity=|mResumedActivity:)[^\n]*\bu(\d+) ([A-Za-z0-9_.]+)/`)

func (a *ADB) state(ctx context.Context) (displayState, error) {
	s := displayState{locked: true, user: -1}
	policy, e := a.shell(ctx, "dumpsys", "window", "policy")
	if e != nil {
		return s, e
	}
	m := keyguardPattern.FindStringSubmatch(policy)
	s.locked = m == nil || m[1] != "false"
	power, e := a.shell(ctx, "dumpsys", "power")
	if e != nil {
		return s, e
	}
	s.screenOn = strings.Contains(power, "Display Power: state=ON")
	activity, e := a.shell(ctx, "dumpsys", "activity", "activities")
	if e != nil {
		return s, e
	}
	m = foregroundPattern.FindStringSubmatch(activity)
	if m != nil {
		s.user, _ = strconv.Atoi(m[1])
		s.packageName = m[2]
	}
	return s, nil
}
func (a *ADB) Start(ctx context.Context) error {
	l, e := acquireLock(filepath.Join(a.directory, a.config.Serial+".lock"))
	if e != nil {
		return block("phone_busy")
	}
	a.lock = l
	raw, e := a.run(ctx, []string{"get-state"}, nil)
	if e != nil {
		return e
	}
	if strings.TrimSpace(string(raw)) != "device" {
		return block("phone_unavailable")
	}
	s, e := a.state(ctx)
	if e != nil {
		return e
	}
	original := s
	a.original = &original
	if s.user >= 0 && s.user != *a.config.User {
		return block("profile_mismatch")
	}
	if s.packageName != "" && s.packageName != a.config.Package {
		return block("phone_busy")
	}
	if s.screenOn && s.locked {
		return block("owner_verification")
	}
	if _, e = a.shell(ctx, "input", "keyevent", "224"); e != nil {
		return e
	}
	a.awakened = true
	s, e = a.state(ctx)
	if e != nil {
		return e
	}
	if s.locked {
		return block("owner_verification")
	}
	resolved, e := a.shell(ctx, "cmd", "package", "resolve-activity", "--brief", "--user", strconv.Itoa(*a.config.User), "-a", "android.intent.action.MAIN", "-c", "android.intent.category.LAUNCHER", "-p", a.config.Package)
	if e != nil {
		return e
	}
	pattern := regexp.MustCompile(`^` + regexp.QuoteMeta(a.config.Package) + `/[A-Za-z0-9_.$]+$`)
	component := ""
	for _, line := range strings.Split(resolved, "\n") {
		if pattern.MatchString(line) {
			component = line
			break
		}
	}
	if component == "" {
		return block("phone_unavailable")
	}
	_, e = a.shell(ctx, "am", "start", "-W", "--user", strconv.Itoa(*a.config.User), "-n", component)
	return e
}
func (a *ADB) guard(ctx context.Context) error {
	s, e := a.state(ctx)
	if e != nil {
		return e
	}
	if s.user != *a.config.User || s.packageName != a.config.Package {
		return block("profile_mismatch")
	}
	if s.locked {
		return block("owner_verification")
	}
	return nil
}
func (a *ADB) Capture(ctx context.Context) (string, error) {
	if e := a.guard(ctx); e != nil {
		return "", e
	}
	stem := filepath.Join(a.evidence, randomID())
	png, e := a.run(ctx, []string{"exec-out", "screencap", "-p"}, nil)
	if e != nil {
		return "", e
	}
	if !bytes.HasPrefix(png, []byte("\x89PNG\r\n\x1a\n")) {
		return "", block("phone_unavailable")
	}
	if e = os.WriteFile(stem+".png", png, 0600); e != nil {
		return "", e
	}
	path := "/data/local/tmp/gowa-companion-" + randomID() + ".xml"
	defer func() {
		clean, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		a.shell(clean, "rm", "-f", path)
	}()
	if _, e = a.shell(ctx, "uiautomator", "dump", "--compressed", path); e != nil {
		return "", e
	}
	source, e := a.shell(ctx, "cat", path)
	if e != nil {
		return "", e
	}
	if e = os.WriteFile(stem+".xml", []byte(source), 0600); e != nil {
		return "", e
	}
	if e = a.guard(ctx); e != nil {
		return "", e
	}
	s, e := ParseScreen(source, a.config.Package)
	if e != nil {
		return "", e
	}
	if len(s.Nodes) > 0 {
		m := boundsPattern.FindStringSubmatch(s.Nodes[0].get("bounds"))
		if m != nil && m[1] == "0" && m[2] == "0" {
			a.width, _ = strconv.Atoi(m[3])
			a.height, _ = strconv.Atoi(m[4])
		}
	}
	return source, nil
}
func (a *ADB) Tap(ctx context.Context, x, y int) error {
	if e := a.guard(ctx); e != nil {
		return e
	}
	if _, e := a.shell(ctx, "input", "tap", strconv.Itoa(x), strconv.Itoa(y)); e != nil {
		return e
	}
	return pause(ctx, time.Second)
}
func (a *ADB) Back(ctx context.Context) error {
	if e := a.guard(ctx); e != nil {
		return e
	}
	if _, e := a.shell(ctx, "input", "keyevent", "4"); e != nil {
		return e
	}
	return pause(ctx, 700*time.Millisecond)
}
func (a *ADB) Swipe(ctx context.Context) error {
	if e := a.guard(ctx); e != nil {
		return e
	}
	if a.width == 0 || a.height == 0 {
		return block("unknown_screen")
	}
	if _, e := a.shell(ctx, "input", "swipe", strconv.Itoa(a.width/2), strconv.Itoa(a.height*85/100), strconv.Itoa(a.width/2), strconv.Itoa(a.height*27/100), "500"); e != nil {
		return e
	}
	return pause(ctx, 700*time.Millisecond)
}
func (a *ADB) Text(ctx context.Context, s string) error {
	if e := a.guard(ctx); e != nil {
		return e
	}
	if !regexp.MustCompile(`^[A-Z0-9]$`).MatchString(s) {
		return block("invalid_request")
	}
	_, e := a.shell(ctx, "input", "text", s)
	return e
}
func (a *ADB) Restore(ctx context.Context, origin string) (err error) {
	if !a.awakened {
		return nil
	}
	defer func() {
		if a.original != nil && !a.original.screenOn {
			if _, e := a.shell(ctx, "input", "keyevent", "223"); e != nil {
				err = e
			}
		}
	}()
	if origin == "" {
		return nil
	}
	for range 6 {
		source, e := a.Capture(ctx)
		if e == nil {
			s, e := ParseScreen(source, a.config.Package)
			if e == nil && s.Title(origin) {
				return nil
			}
		}
		if e != nil {
			b, ok := e.(*Blocked)
			if !ok || b.Reason != "restriction" {
				return e
			}
		}
		if e = a.Back(ctx); e != nil {
			return e
		}
	}
	return block("cleanup_failed")
}
func (a *ADB) Release() error {
	if a.lock != nil {
		e := a.lock.Close()
		a.lock = nil
		return e
	}
	return nil
}
