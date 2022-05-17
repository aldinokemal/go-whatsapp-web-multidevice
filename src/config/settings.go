package config

type Browser string

var (
	AppPort  string = "3000"
	AppDebug bool   = false

	PathQrCode    string = "statics/images/qrcode"
	PathSendItems string = "statics/images/senditems"

	DBName string = "hydrogenWaCli.db"

	WhatsappLogLevel            string = "ERROR"
	WhatsappAutoReplyMessage    string
	WhatsappSettingMaxFileSize  int64 = 10240000 // 10MB
	WhatsappSettingMaxVideoSize int64 = 30000000 // 30MB
)
