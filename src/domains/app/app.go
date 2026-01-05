package app

import (
	"context"
	"time"
)

type IAppUsecase interface {
	Login(ctx context.Context, deviceID string) (response LoginResponse, err error)
	LoginWithCode(ctx context.Context, deviceID string, phoneNumber string) (loginCode string, err error)
	Logout(ctx context.Context, deviceID string) (err error)
	Reconnect(ctx context.Context, deviceID string) (err error)
	Status(ctx context.Context, deviceID string) (isConnected bool, isLoggedIn bool, err error)
	FirstDevice(ctx context.Context) (response DevicesResponse, err error)
	FetchDevices(ctx context.Context) (response []DevicesResponse, err error)
}

type DevicesResponse struct {
	Name   string `json:"name"`
	Device string `json:"device"`
}

type LoginResponse struct {
	ImagePath string        `json:"image_path"`
	Duration  time.Duration `json:"duration"`
	Code      string        `json:"code"`
}
