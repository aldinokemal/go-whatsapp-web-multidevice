package config

import (
	"fmt"
	waProto "go.mau.fi/whatsmeow/binary/proto"
)

var (
	AppVersion             = "v3.11.0"
	AppPort                = "3000"
	AppDebug               = false
	AppOs                  = fmt.Sprintf("AldinoKemal")
	AppPlatform            = waProto.DeviceProps_PlatformType(1)
	AppBasicAuthCredential string
	AppBasicAuthAccount    = make(map[string]string, 0)

	PathQrCode    = "statics/qrcode"
	PathSendItems = "statics/senditems"
	PathStorages  = "storages"

	DBName = "whatsapp.db"

	WhatsappLogLevel            = "ERROR"
	WhatsappAutoReplyMessage    string
	WhatsappAutoReplyWebhook    string
	WhatsappSettingMaxFileSize  int64 = 50000000  // 50MB
	WhatsappSettingMaxVideoSize int64 = 100000000 // 100MB
)
