package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/web"
	"github.com/spf13/cobra"
)

const skipAppInitAnnotation = "skip-app-init"

var adminLinkForce bool

var adminLinkCmd = &cobra.Command{
	Use:   "admin-link <phone>",
	Short: "Print a one-time WTF sign-in link for a phone number (bootstrap and recovery)",
	Long: `Print a one-time sign-in link for a phone number. It works once, for 15 minutes.

Run it from the server's working directory so it opens the server's accounts
database, e.g. on Render:  cd /app && ./whatsapp admin-link <phone>

The number must be in WTF_SUPERADMIN_PHONES, or --force must be given (for
deliberate recovery of an existing ordinary user). The resolved database path is
printed to stderr on every run.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	Annotations:  map[string]string{skipAppInitAnnotation: "true"},
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := loadMultiUserSettings()
		if err != nil {
			return err
		}
		if !s.Enabled {
			return errors.New("WTF_MULTIUSER is not true")
		}
		abs, err := resolveAccountsDB(s.DBURI)
		if err != nil {
			return err
		}
		if abs != "" {
			fmt.Fprintln(os.Stderr, "accounts database:", abs)
		}
		if s.PublicURLIsFallback {
			fmt.Fprintln(os.Stderr, "WARNING: neither WTF_PUBLIC_URL nor RENDER_EXTERNAL_URL is set; the link uses http://localhost:3000 and will not work from a browser elsewhere")
		}
		st, err := store.Open(s.DBURI)
		if err != nil {
			return err
		}
		defer st.Close()
		if err := checkAdminLinkAllowed(cmd.Context(), st, s, args[0], adminLinkForce); err != nil {
			return err
		}
		link, err := mintAdminLink(cmd.Context(), st, s, args[0])
		if err != nil {
			return err
		}
		fmt.Println(link)
		fmt.Fprintln(os.Stderr, adminLinkNote)
		return nil
	},
}

// adminLinkNote is what the command says about the link it printed: the
// same words as the web's Add user and Get sign-in link.
const adminLinkNote = "Open this link, or send it to them. " + web.OneTimeLinkNote

func init() {
	adminLinkCmd.Flags().BoolVar(&adminLinkForce, "force", false, "allow a number that is not in WTF_SUPERADMIN_PHONES")
	rootCmd.AddCommand(adminLinkCmd)
}

// resolveAccountsDB returns the absolute path behind a sqlite "file:" URI and
// fails (naming that path) when its directory is missing, so the command never
// silently works on a different database. In-memory URIs return "".
func resolveAccountsDB(uri string) (string, error) {
	p := strings.TrimPrefix(uri, "file:")
	if i := strings.Index(p, "?"); i >= 0 {
		p = p[:i]
	}
	if p == "" || strings.Contains(p, ":memory:") {
		return "", nil
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(filepath.Dir(abs)); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("accounts database directory does not exist: %s (run from the server's working directory)", filepath.Dir(abs))
	}
	return abs, nil
}

// checkAdminLinkAllowed is read-only. Anyone not on the superadmin allowlist
// needs --force, so a typo cannot create a stray user.
func checkAdminLinkAllowed(ctx context.Context, st *store.Store, s multiuser.Settings, phoneRaw string, force bool) error {
	phone, err := store.NormalizePhone(phoneRaw)
	if err != nil {
		return err
	}
	if multiuser.IsSuperadmin(s, phone) || force {
		return nil
	}
	if len(s.Superadmins) == 0 {
		return errors.New("WTF_SUPERADMIN_PHONES is empty; add the number there (or pass --force to deliberately issue a link anyway)")
	}
	return errors.New("this number is not in WTF_SUPERADMIN_PHONES; pass --force to issue a link anyway")
}

func mintAdminLink(ctx context.Context, st *store.Store, s multiuser.Settings, phoneRaw string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	phone, err := store.NormalizePhone(phoneRaw)
	if err != nil {
		return "", err
	}
	u, err := st.UserByPhone(ctx, phone)
	created := false
	if errors.Is(err, store.ErrNotFound) {
		u, err = st.CreateUser(ctx, phone)
		created = true
	}
	if err != nil {
		return "", err
	}
	if u.Status != store.StatusActive {
		return "", errors.New("this user is disabled; re-enable them before issuing a sign-in link")
	}
	raw, err := st.CreateLoginToken(ctx, phone, 15*time.Minute)
	if err != nil {
		return "", err
	}
	if created {
		_ = st.Audit(ctx, store.AuditEvent{UserID: u.ID, Action: "signup"})
	}
	_ = st.Audit(ctx, store.AuditEvent{UserID: u.ID, Action: "admin_link_minted"})
	return strings.TrimRight(s.PublicURL, "/") + "/signin/verify?t=" + url.QueryEscape(raw), nil
}

// commandSkipsAppInit reports whether the command about to run opted out of
// initApp. initApp connects every WhatsApp session; a second process doing so
// next to the server would fight it for the same sessions.
func commandSkipsAppInit(args []string) bool {
	c, _, err := rootCmd.Find(args)
	return err == nil && c != nil && c.Annotations[skipAppInitAnnotation] == "true"
}
