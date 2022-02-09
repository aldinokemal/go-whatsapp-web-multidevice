package services

import (
	"context"
	"errors"
	"fmt"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/structs"
	"github.com/gofiber/fiber/v2"
	fiberutils "github.com/gofiber/fiber/v2/utils"
	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"os"
	"path/filepath"
	"time"
)

type authServiceImpl struct {
	WaCli *whatsmeow.Client
}

func NewAuthService(waCli *whatsmeow.Client) AuthService {
	return &authServiceImpl{
		WaCli: waCli,
	}
}

func (service authServiceImpl) Login(c *fiber.Ctx) (response structs.LoginResponse, err error) {
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
				response.Duration = evt.Timeout / time.Second
				if evt.Event == "code" {
					qrPath := fmt.Sprintf("%s/scan-qr-%s.png", config.PathQrCode, fiberutils.UUIDv4())
					err = qrcode.WriteFile(evt.Code, qrcode.Medium, 512, qrPath)
					if err != nil {
						fmt.Println(err.Error())
					}
					go func() {
						time.Sleep(5 * time.Second)
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

func (service authServiceImpl) Logout(c *fiber.Ctx) (err error) {
	// delete history
	files, err := filepath.Glob("./history-*")
	if err != nil {
		panic(err)
	}
	fmt.Println(files)
	for _, f := range files {
		err = os.Remove(f)
		if err != nil {
			return err
		}
	}
	// delete qr images
	qrImages, err := filepath.Glob("./statics/images/qrcode/scan-*")
	if err != nil {
		panic(err)
	}
	fmt.Println(qrImages)
	for _, f := range qrImages {
		err = os.Remove(f)
		if err != nil {
			return err
		}
	}

	err = service.WaCli.Logout()
	return
}
