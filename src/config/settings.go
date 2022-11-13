package config

import (
	"fmt"
	waProto "go.mau.fi/whatsmeow/binary/proto"
)

var (
	AppVersion             = "v3.7.0"
	AppPort                = "3000"
	AppDebug               = false
	AppOs                  = fmt.Sprintf("AldinoKemal")
	AppPlatform            = waProto.DeviceProps_PlatformType(1)
	AppBasicAuthCredential string

	PathQrCode    = "statics/qrcode"
	PathSendItems = "statics/senditems"
	PathStorages  = "storages"

	DBName = "hydrogenWaCli.db"

	WhatsappLogLevel            = "ERROR"
	WhatsappAutoReplyMessage    string
	WhatsappAutoReplyWebhook    string
	WhatsappSettingMaxFileSize  int64 = 30000000 // 10MB
	WhatsappSettingMaxVideoSize int64 = 30000000 // 30MB
)
