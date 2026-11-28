package whatsapp

import (
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
)

func TestDeviceOsName(t *testing.T) {
	appOs, appVersion, appendVersion := config.AppOs, config.AppVersion, config.AppOsAppendVersion
	t.Cleanup(func() { config.AppOs, config.AppVersion, config.AppOsAppendVersion = appOs, appVersion, appendVersion })
	config.AppOs, config.AppVersion = "GOWA", "v1.2.3"

	config.AppOsAppendVersion = true
	if got := deviceOsName(); got != "GOWA v1.2.3" {
		t.Fatalf("deviceOsName() = %q, want %q", got, "GOWA v1.2.3")
	}

	config.AppOsAppendVersion = false
	if got := deviceOsName(); got != "GOWA" {
		t.Fatalf("deviceOsName() = %q, want %q", got, "GOWA")
	}
}
