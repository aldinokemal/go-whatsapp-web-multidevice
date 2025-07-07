package usecase

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	fiberUtils "github.com/gofiber/fiber/v2/utils"
	_ "github.com/mattn/go-sqlite3"
	"github.com/sirupsen/logrus"
	"github.com/skip2/go-qrcode"
	"go.mau.fi/libsignal/logger"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
)

type serviceApp struct {
	WaCli *whatsmeow.Client
	db    *sqlstore.Container
}

func NewAppService(waCli *whatsmeow.Client, db *sqlstore.Container) domainApp.IAppUsecase {
	return &serviceApp{
		WaCli: waCli,
		db:    db,
	}
}

func (service serviceApp) Login(_ context.Context) (response domainApp.LoginResponse, err error) {
	if service.WaCli == nil {
		return response, pkgError.ErrWaCLI
	}

	// [DEBUG] Log database state before login
	logrus.Info("[DEBUG] Starting login process...")
	devices, dbErr := service.db.GetAllDevices(context.Background())
	if dbErr != nil {
		logrus.Errorf("[DEBUG] Error getting devices before login: %v", dbErr)
	} else {
		logrus.Infof("[DEBUG] Devices before login: %d found", len(devices))
		for _, device := range devices {
			logrus.Infof("[DEBUG] Device ID: %s, PushName: %s", device.ID.String(), device.PushName)
		}
	}

	// [DEBUG] Log client state
	if service.WaCli.Store.ID != nil {
		logrus.Infof("[DEBUG] Client has existing store ID: %s", service.WaCli.Store.ID.String())
	} else {
		logrus.Info("[DEBUG] Client has no store ID")
	}

	// Disconnect for reconnecting
	service.WaCli.Disconnect()

	chImage := make(chan string)

	logrus.Info("[DEBUG] Attempting to get QR channel...")
	ch, err := service.WaCli.GetQRChannel(context.Background())
	if err != nil {
		logrus.Errorf("[DEBUG] GetQRChannel failed: %v", err)
		logrus.Error(err.Error())
		// This error means that we're already logged in, so ignore it.
		if errors.Is(err, whatsmeow.ErrQRStoreContainsID) {
			logrus.Info("[DEBUG] Error is ErrQRStoreContainsID - attempting to connect")
			_ = service.WaCli.Connect() // just connect to websocket
			if service.WaCli.IsLoggedIn() {
				return response, pkgError.ErrAlreadyLoggedIn
			}
			return response, pkgError.ErrSessionSaved
		} else {
			return response, pkgError.ErrQrChannel
		}
	} else {
		logrus.Info("[DEBUG] QR channel obtained successfully")
		go func() {
			for evt := range ch {
				response.Code = evt.Code
				response.Duration = evt.Timeout / time.Second / 2
				if evt.Event == "code" {
					qrPath := fmt.Sprintf("%s/scan-qr-%s.png", config.PathQrCode, fiberUtils.UUIDv4())
					err = qrcode.WriteFile(evt.Code, qrcode.Medium, 512, qrPath)
					if err != nil {
						logrus.Error("Error when write qr code to file: ", err)
					}
					go func() {
						time.Sleep(response.Duration * time.Second)
						err := os.Remove(qrPath)
						if err != nil {
							// Only log if it's not a "file not found" error
							if !os.IsNotExist(err) {
								logrus.Error("error when remove qrImage file", err.Error())
							}
						}
					}()
					chImage <- qrPath
				} else {
					logrus.Error("error when get qrCode", evt.Event, evt.Error)
				}
			}
		}()
	}

	err = service.WaCli.Connect()
	if err != nil {
		logger.Error("Error when connect to whatsapp", err)
		return response, pkgError.ErrReconnect
	}
	response.ImagePath = <-chImage

	return response, nil
}

func (service serviceApp) LoginWithCode(ctx context.Context, phoneNumber string) (loginCode string, err error) {
	if err = validations.ValidateLoginWithCode(ctx, phoneNumber); err != nil {
		logrus.Errorf("Error when validate login with code: %s", err.Error())
		return loginCode, err
	}

	// detect is already logged in
	if service.WaCli.Store.ID != nil {
		logrus.Warn("User is already logged in")
		return loginCode, pkgError.ErrAlreadyLoggedIn
	}

	// reconnect first
	_ = service.Reconnect(ctx)

	loginCode, err = service.WaCli.PairPhone(ctx, phoneNumber, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	if err != nil {
		logrus.Errorf("Error when pairing phone: %s", err.Error())
		return loginCode, err
	}

	logrus.Infof("Successfully paired phone with code: %s", loginCode)
	return loginCode, nil
}

func (service *serviceApp) Logout(ctx context.Context) (err error) {
	// [DEBUG] Log database state before logout
	logrus.Info("[DEBUG] Starting logout process...")
	devices, dbErr := service.db.GetAllDevices(ctx)
	if dbErr != nil {
		logrus.Errorf("[DEBUG] Error getting devices before logout: %v", dbErr)
	} else {
		logrus.Infof("[DEBUG] Devices before logout: %d found", len(devices))
		for _, device := range devices {
			logrus.Infof("[DEBUG] Device ID: %s, PushName: %s", device.ID.String(), device.PushName)
		}
	}

	// [DEBUG] Call WhatsApp client logout first to disconnect from server
	logrus.Info("[DEBUG] Calling WhatsApp client logout...")
	err = service.WaCli.Logout(ctx)
	if err != nil {
		logrus.Errorf("[DEBUG] WhatsApp logout failed: %v", err)
		// Continue with cleanup even if logout fails
	} else {
		logrus.Info("[DEBUG] WhatsApp logout completed successfully")
	}

	// [DEBUG] Verify devices after logout
	devices, dbErr = service.db.GetAllDevices(ctx)
	if dbErr != nil {
		logrus.Errorf("[DEBUG] Error getting devices after logout: %v", dbErr)
	} else {
		logrus.Infof("[DEBUG] Devices after logout: %d found", len(devices))
	}

	// Disconnect current client
	service.WaCli.Disconnect()

	// Clean up database by removing the file
	// This prevents foreign key constraint issues on next login
	dbPath := strings.TrimPrefix(config.DBURI, "file:")
	if strings.Contains(dbPath, "?") {
		dbPath = strings.Split(dbPath, "?")[0]
	}

	logrus.Infof("[DEBUG] Removing database file to prevent FK constraints: %s", dbPath)
	if err := os.Remove(dbPath); err != nil {
		if !os.IsNotExist(err) {
			logrus.Errorf("[DEBUG] Error removing database file: %v", err)
		} else {
			logrus.Info("[DEBUG] Database file already removed")
		}
	} else {
		logrus.Info("[DEBUG] Database file removed successfully")
	}

	// Reinitialize database and client to avoid restart requirement
	logrus.Info("[DEBUG] Reinitializing database and client...")
	newDB := whatsapp.InitWaDB(ctx)
	newCli := whatsapp.InitWaCLI(ctx, newDB)

	// Update service references
	service.db = newDB
	service.WaCli = newCli

	logrus.Info("[DEBUG] Database and client reinitialized successfully")

	// delete history files
	files, err := filepath.Glob(fmt.Sprintf("./%s/history-*", config.PathStorages))
	if err != nil {
		return err
	}

	for _, f := range files {
		err = os.Remove(f)
		if err != nil {
			return err
		}
	}

	// delete qr images
	qrImages, err := filepath.Glob(fmt.Sprintf("./%s/scan-*", config.PathQrCode))
	if err != nil {
		return err
	}

	for _, f := range qrImages {
		err = os.Remove(f)
		if err != nil {
			return err
		}
	}

	// delete senditems
	qrItems, err := filepath.Glob(fmt.Sprintf("./%s/*", config.PathSendItems))
	if err != nil {
		return err
	}

	for _, f := range qrItems {
		if !strings.Contains(f, ".gitignore") {
			err = os.Remove(f)
			if err != nil {
				return err
			}
		}
	}

	logrus.Info("[DEBUG] Logout process completed successfully")
	logrus.Info("[DEBUG] Application is ready for next login without restart")
	return nil
}

func (service serviceApp) Reconnect(_ context.Context) (err error) {
	service.WaCli.Disconnect()
	return service.WaCli.Connect()
}

func (service serviceApp) FirstDevice(ctx context.Context) (response domainApp.DevicesResponse, err error) {
	if service.WaCli == nil {
		return response, pkgError.ErrWaCLI
	}

	devices, err := service.db.GetFirstDevice(ctx)
	if err != nil {
		return response, err
	}

	response.Device = devices.ID.String()
	if devices.PushName != "" {
		response.Name = devices.PushName
	} else {
		response.Name = devices.BusinessName
	}

	return response, nil
}

func (service serviceApp) FetchDevices(ctx context.Context) (response []domainApp.DevicesResponse, err error) {
	if service.WaCli == nil {
		return response, pkgError.ErrWaCLI
	}

	devices, err := service.db.GetAllDevices(ctx)
	if err != nil {
		return nil, err
	}

	for _, device := range devices {
		var d domainApp.DevicesResponse
		d.Device = device.ID.String()
		if device.PushName != "" {
			d.Name = device.PushName
		} else {
			d.Name = device.BusinessName
		}

		response = append(response, d)
	}

	return response, nil
}
