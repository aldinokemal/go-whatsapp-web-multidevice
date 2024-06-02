package config

import (
	"os"
	"strconv"

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

	WhatsappAutoReplyMessage    string = getEnv("WhatsappAutoReplyMessage", "")
	WhatsappWebhook             string = getEnv("WhatsappWebhook", "")
	WhatsappLogLevel            string = getEnv("WhatsappLogLevel", "ERROR")
	WhatsappSettingMaxFileSize  int64  = getEnvAsInt64("WhatsappSettingMaxFileSize", 50000000)  // 50MB
	WhatsappSettingMaxVideoSize int64  = getEnvAsInt64("WhatsappSettingMaxVideoSize", 100000000) // 100MB
	WhatsappTypeUser            string = "@s.whatsapp.net"
	WhatsappTypeGroup           string = "@g.us"
)

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getEnvAsInt64(key string, defaultValue int64) int64 {
	if valueStr, exists := os.LookupEnv(key); exists {
		if value, err := strconv.ParseInt(valueStr, 10, 64); err == nil {
			return value
		} else {
			log.Printf("Error parsing environment variable %s: %v", key, err)
		}
	}
	return defaultValue
}
}
