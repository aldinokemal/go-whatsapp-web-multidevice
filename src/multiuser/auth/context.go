package auth

import (
	"context"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
)

type ctxKey struct{}

func WithUser(ctx context.Context, u *store.User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

func UserFrom(ctx context.Context) (*store.User, bool) {
	u, ok := ctx.Value(ctxKey{}).(*store.User)
	return u, ok && u != nil
}
