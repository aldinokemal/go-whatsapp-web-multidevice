package config

import (
	"fmt"
	waProto "go.mau.fi/whatsmeow/binary/proto"
)

type Browser string

var (
	AppVersion  string                           = "v3.4.0"
	AppPort     string                           = "3000"
	AppDebug    bool                             = false
	AppOs       string                           = fmt.Sprintf("AldinoKemal")
	AppPlatform waProto.DeviceProps_PlatformType = waProto.DeviceProps_PlatformType(1)

	PathQrCode    string = "statics/qrcode"
	PathSendItems string = "statics/senditems"

	DBName string = "hydrogenWaCli.db"

	WhatsappLogLevel            string = "ERROR"
	WhatsappAutoReplyMessage    string
	WhatsappSettingMaxFileSize  int64 = 30000000 // 10MB
	WhatsappSettingMaxVideoSize int64 = 30000000 // 30MB
)
