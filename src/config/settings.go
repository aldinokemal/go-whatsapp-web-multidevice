package config

import (
	"encoding/json"
	"net/http"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
)

var (
	AppVersion             = "v4.18.0"
	AppPort                = "3000"
	AppDebug               = false
	AppOs                  = "AldinoKemal"
	AppPlatform            = waCompanionReg.DeviceProps_PlatformType(1)
	AppBasicAuthCredential string

	PathQrCode    = "statics/qrcode"
	PathSendItems = "statics/senditems"
	PathMedia     = "statics/media"
	PathStorages  = "storages"

	DBName = "whatsapp.db"

	WhatsappAutoReplyMessage    string
	WhatsappWebhook             string
	WhatsappWebhookSecret             = "secret"
	WhatsappLogLevel                  = "ERROR"
	WhatsappSettingMaxFileSize  int64 = 50000000  // 50MB
	WhatsappSettingMaxVideoSize int64 = 100000000 // 100MB
	WhatsappTypeUser                  = "@s.whatsapp.net"
	WhatsappTypeGroup                 = "@g.us"
	WhatsappAccountValidation         = true
	IgnoreExtractMedia                = false // Skip sync media to storages folder (jpg ...)
	IgnoreExtractHistory              = false // Skip sync history to storages folder
	NumberFormatLocale                = "" // Empty to maintain the current process or "Brazil" to use the WhatsApp number pattern for the country
)

func envHandler(w http.ResponseWriter, r *http.Request) { // exposes the NumberFormatLocal variable for use in JS functions 
	response := map[string]string{
		"NumberFormatLocale": NumberFormatLocale,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func config() {
	http.HandleFunc("/api/config", envHandler) 
	http.ListenAndServe(":"+AppPort, nil) 
}
