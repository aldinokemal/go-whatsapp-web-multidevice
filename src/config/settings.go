package config

import (
	"log"
	"os"
	"github.com/joho/godotenv"
	waProto "go.mau.fi/whatsmeow/binary/proto"
)

var (
	AppVersion             = "v4.14.0"
	AppPort                = "3000"
	AppDebug               = false
	AppOs                  = "AldinoKemal"
	AppPlatform            = waProto.DeviceProps_PlatformType(1)
	AppBasicAuthCredential string

	PathQrCode    = "statics/qrcode"
	PathSendItems = "statics/senditems"
	PathMedia     = "statics/media"
	PathStorages  = "storages"

	DBName = "whatsapp.db"

	WhatsappAutoReplyMessage    string
	WhatsappWebhook             string
	WhatsappLogLevel                  = "ERROR"
	WhatsappSettingMaxFileSize  int64 = 50000000  // 50MB
	WhatsappSettingMaxVideoSize int64 = 100000000 // 100MB
	WhatsappTypeUser                  = "@s.whatsapp.net"
	WhatsappTypeGroup                 = "@g.us"
)

func init() {
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}
	WhatsappWebhook = os.Getenv("WhatsappWebhook")
	if WhatsappWebhook == "" {
		log.Fatalf("WhatsappWebhook environment variable is not set")
	}
}
