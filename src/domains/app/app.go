package app

import (
	"context"
)

type IAppService interface {
	Login(ctx context.Context) (response LoginResponse, err error)
	Logout(ctx context.Context) (err error)
	Reconnect(ctx context.Context) (err error)
}
