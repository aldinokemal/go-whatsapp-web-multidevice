package wanode

import (
	"fmt"
	"strconv"

	waBinary "go.mau.fi/whatsmeow/binary"
)

func NodeChildren(n *waBinary.Node) []waBinary.Node {
	if n == nil {
		return nil
	}
	if children, ok := n.Content.([]waBinary.Node); ok {
		return children
	}
	return nil
}

func NodeBytes(n *waBinary.Node) []byte {
	if n == nil {
		return nil
	}
	if b, ok := n.Content.([]byte); ok {
		return b
	}
	return nil
}

func AttrString(attrs waBinary.Attrs, key string) string {
	v, ok := attrs[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	case int64:
		return strconv.FormatInt(t, 10)
	case int:
		return strconv.Itoa(t)
	case uint64:
		return strconv.FormatUint(t, 10)
	case bool:
		return strconv.FormatBool(t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func AttrInt(attrs waBinary.Attrs, key string, fallback int) int {
	s := AttrString(attrs, key)
	if s == "" {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}

func HasAttr(attrs waBinary.Attrs, key string) bool {
	v, ok := attrs[key]
	return ok && v != nil && AttrString(attrs, key) != ""
}
