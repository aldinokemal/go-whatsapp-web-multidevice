package services

import (
	"context"
	"errors"
	"fmt"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	fiberUtils "github.com/gofiber/fiber/v2/utils"
	"github.com/sirupsen/logrus"
	"github.com/skip2/go-qrcode"
	"go.mau.fi/libsignal/logger"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type serviceApp struct {
	WaCli *whatsmeow.Client
	db    *sqlstore.Container
}

func NewAppService(waCli *whatsmeow.Client, db *sqlstore.Container) domainApp.IAppService {
	return &serviceApp{
		WaCli: waCli,
		db:    db,
	}
}

func (service serviceApp) Login(_ context.Context) (response domainApp.LoginResponse, err error) {
	if service.WaCli == nil {
		return response, pkgError.ErrWaCLI
	}

	// Disconnect for reconnecting
	service.WaCli.Disconnect()

	chImage := make(chan string)

	ch, err := service.WaCli.GetQRChannel(context.Background())
	if err != nil {
		logrus.Error(err.Error())
		// This error means that we're already logged in, so ignore it.
		if errors.Is(err, whatsmeow.ErrQRStoreContainsID) {
			_ = service.WaCli.Connect() // just connect to websocket
			if service.WaCli.IsLoggedIn() {
				return response, pkgError.ErrAlreadyLoggedIn
			}
			return response, pkgError.ErrSessionSaved
		} else {
			return response, pkgError.ErrQrChannel
		}
	} else {
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
							logrus.Error("error when remove qrImage file", err.Error())
						}
					}()
					chImage <- qrPath
				} else {
					logrus.Error("error when get qrCode", evt.Event)
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

func (service serviceApp) Logout(_ context.Context) (err error) {
	// delete history
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

	err = service.WaCli.Logout()
	return
}

func (service serviceApp) Reconnect(_ context.Context) (err error) {
	service.WaCli.Disconnect()
	return service.WaCli.Connect()
}

func (service serviceApp) FetchDevices(_ context.Context) (response []domainApp.FetchDevicesResponse, err error) {
	if service.WaCli == nil {
		return response, pkgError.ErrWaCLI
	}

	devices, err := service.db.GetAllDevices()
	if err != nil {
		return nil, err
	}

	for _, device := range devices {
		var d domainApp.FetchDevicesResponse
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
