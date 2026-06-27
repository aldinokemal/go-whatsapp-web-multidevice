package wanode

import (
	"strings"

	"go.mau.fi/whatsmeow/types"
)

func CleanJID(jid string) string {
	if i := strings.Index(jid, ":"); i >= 0 {
		if at := strings.Index(jid, "@"); at > i {
			return jid[:i] + jid[at:]
		}
	}
	return jid
}

func MustJID(s string) types.JID {
	j, err := types.ParseJID(s)
	if err != nil {
		return types.JID{}
	}
	return j
}
