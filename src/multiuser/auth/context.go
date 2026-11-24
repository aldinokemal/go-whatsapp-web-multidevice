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

type actorKey struct{}

// WithActor marks ctx as an action an admin is taking on the context's user's
// behalf (the user in WithUser is the one acted on). Audit lines read it to
// name who did it. It grants nothing: ownership checks still use WithUser.
func WithActor(ctx context.Context, admin *store.User) context.Context {
	return context.WithValue(ctx, actorKey{}, admin)
}

func ActorFrom(ctx context.Context) (*store.User, bool) {
	u, ok := ctx.Value(actorKey{}).(*store.User)
	return u, ok && u != nil
}
