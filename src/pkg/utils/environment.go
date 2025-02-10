package utils

import (
	"context"
	"fmt"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// IsLocal will return true if the APP_ENV is not listed in those three condition
func IsLocal() bool {
	envLevel := MustHaveEnv("APP_ENV")
	return envLevel != "production" && envLevel != "staging" && envLevel != "integration"
}

func Env[T any](key string, defValue ...T) T {
	env := viper.Get(key)
	if env == nil && len(defValue) > 0 {
		return defValue[0]
	}
	return env.(T)
}

// MustHaveEnv ensure the ENV is exists, otherwise will crashing the app
func MustHaveEnv(key string) string {
	env := viper.GetString(key)
	if env == "" {
		log.Warn(context.Background(), map[string]interface{}{
			"field": key,
		}, "variable is not well set, reading from .env file")
		viper.SetConfigFile(".env")
		viper.SetConfigType("env")
		err := viper.ReadInConfig()
		if err != nil {
			log.Fatal(err, "can't read .env file")
		}
		env = viper.GetString(key)
	}
	if env == "" {
		log.Fatal(fmt.Sprintf("%s is not well set", key))
	}
	return env
}

// MustHaveEnvBool ensure the ENV exists and returns a bool, otherwise it will crash the app
func MustHaveEnvBool(key string) bool {
	env := MustHaveEnv(key)
	return env == "true"
}

// MustHaveEnvInt ensure the ENV is exists and return int value
func MustHaveEnvInt(key string) int {
	env := MustHaveEnv(key)
	number, err := strconv.ParseInt(env, 10, 64)
	if err != nil {
		log.Fatal(fmt.Sprintf("%s is not well set", key))
	}
	return int(number)
}

// MustHaveEnvMinuteDuration ensure the ENV is exists and return a minute duration
func MustHaveEnvMinuteDuration(key string) time.Duration {
	env := MustHaveEnv(key)
	number, err := strconv.ParseInt(env, 10, 64)
	if err != nil {
		log.Fatal(fmt.Sprintf("%s is not well set", key))
	}

	return time.Duration(number) * time.Minute
}

func LoadConfig(path string, name ...string) (err error) {
	viper.AddConfigPath(path)
	if len(name) > 0 && name[0] != "" {
		viper.SetConfigName(name[0])
	} else {
		viper.SetConfigName(".env")
	}

	viper.SetConfigType("env")
	viper.AutomaticEnv()

	err = viper.ReadInConfig()
	if err != nil {
		return
	}
	return nil
}
