package utils

import "github.com/spf13/viper"

// LoadConfig reads configuration from a .env-style file in the given path.
// If a custom name is supplied, it is used as the config name; otherwise ".env" is used.
func LoadConfig(path string, name ...string) error {
	viper.AddConfigPath(path)
	if len(name) > 0 && name[0] != "" {
		viper.SetConfigName(name[0])
	} else {
		viper.SetConfigName(".env")
	}

	viper.SetConfigType("env")
	viper.AutomaticEnv()

	return viper.ReadInConfig()
}
