package helpers

import (
	"context"
	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"mime/multipart"
	"time"
)

func SetAutoConnectAfterBooting(service domainApp.IAppService) {
	time.Sleep(2 * time.Second)
	_ = service.Reconnect(context.Background())
}

func MultipartFormFileHeaderToBytes(fileHeader *multipart.FileHeader) []byte {
	file, _ := fileHeader.Open()
	defer file.Close()

	fileBytes := make([]byte, fileHeader.Size)
	_, _ = file.Read(fileBytes)

	return fileBytes
}
