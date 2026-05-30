package utils_test

import (
	"os"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestLoadConfig(t *testing.T) {
	t.Run("default name", func(t *testing.T) {
		viper.Reset()
		tempDir := t.TempDir()
		require := assert.New(t)

		require.NoError(os.WriteFile(tempDir+"/.env", []byte("TEST_CONFIG=config_value\n"), 0644))

		err := utils.LoadConfig(tempDir)
		require.NoError(err)
		require.Equal("config_value", viper.GetString("TEST_CONFIG"))
	})

	t.Run("custom name", func(t *testing.T) {
		viper.Reset()
		tempDir := t.TempDir()
		require := assert.New(t)

		require.NoError(os.WriteFile(tempDir+"/custom.env", []byte("CUSTOM_CONFIG=custom_value\n"), 0644))

		err := utils.LoadConfig(tempDir, "custom")
		require.NoError(err)
		require.Equal("custom_value", viper.GetString("CUSTOM_CONFIG"))
	})

	t.Run("non-existent directory returns error", func(t *testing.T) {
		viper.Reset()
		err := utils.LoadConfig("/non/existent/directory")
		assert.Error(t, err)
	})
}
