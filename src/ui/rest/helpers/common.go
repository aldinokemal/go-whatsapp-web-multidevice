package helpers

import (
	"context"
	"mime/multipart"
	"time"

	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
)

func SetAutoConnectAfterBooting(service domainApp.IAppUsecase) {
	time.Sleep(2 * time.Second)
	devices, err := service.FetchDevices(context.Background())
	if err != nil || len(devices) == 0 {
		logrus.Warn("auto-connect skipped: no devices available")
		return
	}
	for _, device := range devices {
		if err := service.Reconnect(context.Background(), device.Device); err != nil {
			logrus.Warnf("auto-connect failed for device %s: %v", device.Device, err)
		} else {
			logrus.Infof("auto-connected device %s", device.Device)
		}
	}
}

func SetAutoReconnectChecking(cli *whatsmeow.Client) {
	if cli == nil {
		logrus.Warn("SetAutoReconnectChecking was called with a nil WhatsApp client; skipping auto-reconnect loop")
		return
	}
	// Run every 5 minutes to check if the connection is still alive, if not, reconnect
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			if !cli.IsConnected() {
				_ = cli.Connect()
			}
		}
	}()
}

func MultipartFormFileHeaderToBytes(fileHeader *multipart.FileHeader) []byte {
	file, _ := fileHeader.Open()
	defer file.Close()

	fileBytes := make([]byte, fileHeader.Size)
	_, _ = file.Read(fileBytes)

	return fileBytes
}
