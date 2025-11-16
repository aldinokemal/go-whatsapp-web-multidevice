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
	_ = service.Reconnect(context.Background())
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
