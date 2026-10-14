package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/spf13/cobra"
)

const skipAppInitAnnotation = "skip-app-init"

var adminLinkCmd = &cobra.Command{
	Use:         "admin-link <phone>",
	Short:       "Print a one-time WTF sign-in link for a phone number (bootstrap and recovery)",
	Args:        cobra.ExactArgs(1),
	Annotations: map[string]string{skipAppInitAnnotation: "true"},
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := loadMultiUserSettings()
		if err != nil {
			return err
		}
		if !s.Enabled {
			return errors.New("WTF_MULTIUSER is not true")
		}
		st, err := store.Open(s.DBURI)
		if err != nil {
			return err
		}
		defer st.Close()
		link, err := mintAdminLink(cmd.Context(), st, s, args[0])
		if err != nil {
			return err
		}
		phone, _ := store.NormalizePhone(args[0])
		if !multiuser.IsSuperadmin(s, phone) {
			fmt.Fprintln(os.Stderr, "note: this number is not in WTF_SUPERADMIN_PHONES, so it signs in as an ordinary user")
		}
		fmt.Println(link)
		fmt.Fprintln(os.Stderr, "valid once, for 15 minutes")
		return nil
	},
}

func init() { rootCmd.AddCommand(adminLinkCmd) }

func mintAdminLink(ctx context.Context, st *store.Store, s multiuser.Settings, phoneRaw string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	phone, err := store.NormalizePhone(phoneRaw)
	if err != nil {
		return "", err
	}
	u, err := st.UserByPhone(ctx, phone)
	if errors.Is(err, store.ErrNotFound) {
		u, err = st.CreateUser(ctx, phone)
	}
	if err != nil {
		return "", err
	}
	if u.Status == store.StatusDisabled {
		return "", errors.New("this user is disabled; re-enable them before issuing a sign-in link")
	}
	raw, err := st.CreateLoginToken(ctx, phone, 15*time.Minute)
	if err != nil {
		return "", err
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
