package helpers

import (
	"fmt"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"net/http"
	"time"
)

func SetAutoConnectAfterBooting() {
	time.Sleep(2 * time.Second)
	_, _ = http.Get(fmt.Sprintf("http://localhost:%s/app/reconnect", config.AppPort))
}
