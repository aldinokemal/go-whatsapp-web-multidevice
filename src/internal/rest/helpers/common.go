package helpers

import (
	"context"
	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"time"
)

func SetAutoConnectAfterBooting(service domainApp.IAppService) {
	time.Sleep(2 * time.Second)
	_ = service.Reconnect(context.Background())
}
