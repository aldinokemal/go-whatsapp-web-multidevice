package services

import (
	"context"
	"errors"
	"fmt"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	fiberUtils "github.com/gofiber/fiber/v2/utils"
	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"os"
	"path/filepath"
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
		return response, errors.New("wa cli nil cok")
	}

	// Disconnect for reconnecting
	service.WaCli.Disconnect()

	chImage := make(chan string)

	ch, err := service.WaCli.GetQRChannel(context.Background())
	if err != nil {
		// This error means that we're already logged in, so ignore it.
		if errors.Is(err, whatsmeow.ErrQRStoreContainsID) {
			_ = service.WaCli.Connect() // just connect to websocket
			if service.WaCli.IsLoggedIn() {
				return response, errors.New("you already logged in :)")
			}
			return response, errors.New("your session have been saved, please wait to connect 2 second and refresh again")
		} else {
			return response, errors.New("Error when GetQRChannel:" + err.Error())
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
						fmt.Println("error when write qrImage file", err.Error())
					}
					go func() {
						time.Sleep(response.Duration * time.Second)
						err := os.Remove(qrPath)
						if err != nil {
							fmt.Println("Failed to remove qrPath " + qrPath)
						}
					}()
					chImage <- qrPath
				} else {
					fmt.Printf("QR channel result: %s", evt.Event)
				}
			}
		}()
	}

	err = service.WaCli.Connect()
	if err != nil {
		return response, errors.New("Failed to connect bro " + err.Error())
	}
	response.ImagePath = <-chImage

	return response, nil
}

func (service serviceApp) Logout(_ context.Context) (err error) {
	// delete history
	files, err := filepath.Glob("./history-*")
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
	qrImages, err := filepath.Glob("./statics/images/qrcode/scan-*")
	if err != nil {
		return err
	}

	for _, f := range qrImages {
		err = os.Remove(f)
		if err != nil {
			return err
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
		return response, errors.New("wa cli nil cok")
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
