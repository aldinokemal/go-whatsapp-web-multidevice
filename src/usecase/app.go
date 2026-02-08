package usecase

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/websocket"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	fiberUtils "github.com/gofiber/fiber/v2/utils"
	_ "github.com/mattn/go-sqlite3"
	"github.com/sirupsen/logrus"
	"github.com/skip2/go-qrcode"
	"go.mau.fi/libsignal/logger"
	"go.mau.fi/whatsmeow"
)

type serviceApp struct {
	chatStorageRepo domainChatStorage.IChatStorageRepository
	deviceManager   *whatsapp.DeviceManager
}

func NewAppService(chatStorageRepo domainChatStorage.IChatStorageRepository, deviceManager *whatsapp.DeviceManager) domainApp.IAppUsecase {
	return &serviceApp{
		chatStorageRepo: chatStorageRepo,
		deviceManager:   deviceManager,
	}
}

func (service *serviceApp) Login(ctx context.Context, deviceID string) (response domainApp.LoginResponse, err error) {
	instance, client, err := service.ensureClient(ctx, deviceID)
	if err != nil {
		return response, err
	}

	if client.IsLoggedIn() {
		instance.UpdateStateFromClient()
		return response, pkgError.ErrAlreadyLoggedIn
	}

	// Disconnect first to ensure QR flow starts cleanly.
	client.Disconnect()

	// Use a detached context for the QR channel so the pairing session
	// survives after the HTTP response is sent. The HTTP request context
	// has a short timeout (e.g. 45s) which would cancel the QR emitter
	// and disconnect the client before the user can scan the code.
	// Total QR window: ~160s (first code 60s + five codes at 20s each).
	qrCtx, qrCancel := context.WithTimeout(context.Background(), 3*time.Minute)

	chImage := make(chan string, 1) // Buffered to prevent goroutine leak
	ch, err := client.GetQRChannel(qrCtx)
	if err != nil {
		qrCancel()
		logrus.Errorf("[LOGIN][%s] GetQRChannel failed: %v", deviceID, err)
		if errors.Is(err, whatsmeow.ErrQRStoreContainsID) {
			_ = client.Connect()
			instance.UpdateStateFromClient()
			if client.IsLoggedIn() {
				return response, pkgError.ErrAlreadyLoggedIn
			}
			return response, pkgError.ErrSessionSaved
		}
		return response, pkgError.ErrQrChannel
	}

	go func() {
		defer qrCancel()
		defer close(chImage) // Ensure channel is closed when done
		for evt := range ch {
			response.Code = evt.Code
			response.Duration = evt.Timeout / time.Second / 2
			if evt.Event == "code" {
				qrPath := fmt.Sprintf("%s/scan-qr-%s.png", config.PathQrCode, fiberUtils.UUIDv4())
				if err := qrcode.WriteFile(evt.Code, qrcode.Medium, 512, qrPath); err != nil {
					logrus.Errorf("[LOGIN][%s] Error when write qr code to file: %v", deviceID, err)
					continue // Skip sending if QR generation failed
				}
				go func(path string, duration time.Duration) {
					time.Sleep(duration * time.Second)
					if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
						logrus.Errorf("[LOGIN][%s] error when remove qrImage file: %v", deviceID, err)
					}
				}(qrPath, response.Duration)
				select {
				case chImage <- qrPath:
				case <-qrCtx.Done():
					logrus.Warnf("[LOGIN][%s] QR context canceled while sending QR path", deviceID)
					return
				}
			} else {
				logrus.Errorf("[LOGIN][%s] error when get qrCode %s %v", deviceID, evt.Event, evt.Error)
			}
		}
	}()

	if err = client.Connect(); err != nil {
		qrCancel()
		logger.Error("Error when connect to whatsapp", err)
		return response, pkgError.ErrReconnect
	}

	instance.UpdateStateFromClient()

	// Wait for QR image with timeout to prevent hanging
	select {
	case imagePath, ok := <-chImage:
		if !ok {
			return response, fmt.Errorf("QR channel closed without receiving image")
		}
		response.ImagePath = imagePath
	case <-ctx.Done():
		return response, ctx.Err()
	case <-time.After(120 * time.Second):
		return response, fmt.Errorf("timeout waiting for QR code")
	}

	return response, nil
}

func (service *serviceApp) LoginWithCode(ctx context.Context, deviceID string, phoneNumber string) (loginCode string, err error) {
	if err = validations.ValidateLoginWithCode(ctx, phoneNumber); err != nil {
		logrus.Errorf("Error when validate login with code: %s", err.Error())
		return loginCode, err
	}

	instance, client, err := service.ensureClient(ctx, deviceID)
	if err != nil {
		return loginCode, err
	}

	if client.IsLoggedIn() {
		instance.UpdateStateFromClient()
		return loginCode, pkgError.ErrAlreadyLoggedIn
	}

	// Connect before requesting pairing code.
	if !client.IsConnected() {
		if err = client.Connect(); err != nil {
			return loginCode, err
		}
	}

	logrus.Infof("[LOGIN_CODE][%s] Starting phone pairing for number: %s", deviceID, phoneNumber)
	loginCode, err = client.PairPhone(ctx, phoneNumber, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	if err != nil {
		logrus.Errorf("Error when pairing phone: %s", err.Error())
		return loginCode, err
	}

	instance.UpdateStateFromClient()
	logrus.Infof("Successfully paired phone with code: %s", loginCode)
	return loginCode, nil
}

func (service *serviceApp) Logout(ctx context.Context, deviceID string) error {
	if service.deviceManager == nil {
		return fmt.Errorf("device manager not initialized")
	}

	if err := service.deviceManager.PurgeDevice(ctx, deviceID); err != nil {
		logrus.WithError(err).Warnf("[LOGOUT][%s] purge completed with warnings", deviceID)
		return err
	}

	// Broadcast device removal so UI can refresh without manual polling
	var devices []domainApp.DevicesResponse
	if list, err := service.FetchDevices(ctx); err == nil {
		devices = list
	} else {
		logrus.WithError(err).Warn("[LOGOUT] failed to fetch devices after purge")
	}

	websocket.Broadcast <- websocket.BroadcastMessage{
		Code:    "DEVICE_REMOVED",
		Message: fmt.Sprintf("Device %s logged out and removed", deviceID),
		Result: map[string]any{
			"device_id": deviceID,
			"devices":   devices,
		},
	}

	return nil
}

func (service *serviceApp) Reconnect(_ context.Context, deviceID string) (err error) {
	instance, client, err := service.ensureClient(context.Background(), deviceID)
	if err != nil {
		return err
	}

	if client.Store == nil || client.Store.ID == nil {
		return fmt.Errorf("device %s is not logged in (session deleted)", deviceID)
	}

	client.Disconnect()
	err = client.Connect()
	instance.UpdateStateFromClient()
	if err != nil {
		logrus.Errorf("[RECONNECT][%s] Reconnect failed: %v", deviceID, err)
	}
	return err
}

func (service *serviceApp) Status(_ context.Context, deviceID string) (bool, bool, error) {
	if service.deviceManager == nil {
		return false, false, fmt.Errorf("device manager not initialized")
	}

	instance, ok := service.deviceManager.GetDevice(deviceID)
	if !ok || instance == nil {
		return false, false, fmt.Errorf("device %s not found", deviceID)
	}

	instance.UpdateStateFromClient()
	client := instance.GetClient()
	if client == nil {
		return false, false, nil
	}

	if client.Store == nil || client.Store.ID == nil {
		return false, false, nil
	}

	return client.IsConnected(), client.IsLoggedIn(), nil
}

func (service *serviceApp) FirstDevice(ctx context.Context) (response domainApp.DevicesResponse, err error) {
	devices, err := service.FetchDevices(ctx)
	if err != nil {
		return response, err
	}
	if len(devices) == 0 {
		return response, fmt.Errorf("no devices available")
	}
	return devices[0], nil
}

func (service *serviceApp) FetchDevices(_ context.Context) (response []domainApp.DevicesResponse, err error) {
	if service.deviceManager == nil {
		return response, fmt.Errorf("device manager not initialized")
	}

	for _, inst := range service.deviceManager.ListDevices() {
		inst.UpdateStateFromClient()
		name := inst.DisplayName()
		if name == "" {
			name = inst.PhoneNumber()
		}

		response = append(response, domainApp.DevicesResponse{
			Name:   name,
			Device: inst.ID(),
		})
	}

	return response, nil
}

func (service *serviceApp) ensureClient(ctx context.Context, deviceID string) (*whatsapp.DeviceInstance, *whatsmeow.Client, error) {
	if deviceID == "" {
		return nil, nil, fmt.Errorf("device id is required")
	}

	if service.deviceManager == nil {
		return nil, nil, fmt.Errorf("device manager not initialized")
	}

	instance, err := service.deviceManager.EnsureClient(ctx, deviceID)
	if err != nil {
		return nil, nil, err
	}

	client := instance.GetClient()
	if client == nil {
		return instance, nil, pkgError.ErrWaCLI
	}

	return instance, client, nil
}
