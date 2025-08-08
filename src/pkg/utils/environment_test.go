package utils_test

import (
	"os"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type EnvironmentTestSuite struct {
	suite.Suite
}

func (suite *EnvironmentTestSuite) SetupTest() {
	// Clear any existing viper configs
	viper.Reset()
	// Set up automatic environment variable reading
	viper.AutomaticEnv()
}

func (suite *EnvironmentTestSuite) TearDownTest() {
	viper.Reset()
}

func (suite *EnvironmentTestSuite) TestIsLocal() {
	tests := []struct {
		name     string
		envValue string
		expected bool
	}{
		{"Production environment", "production", false},
		{"Staging environment", "staging", false},
		{"Integration environment", "integration", false},
		{"Development environment", "development", true},
		{"Local environment", "local", true},
	}

	for _, tt := range tests {
		suite.T().Run(tt.name, func(t *testing.T) {
			// Set the environment value
			if tt.envValue != "" {
				viper.Set("APP_ENV", tt.envValue)
			} else {
				viper.Set("APP_ENV", nil) // Explicitly clear the value
			}

			result := utils.IsLocal()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func (suite *EnvironmentTestSuite) TestEnv() {
	// Test with existing value
	viper.Set("TEST_KEY", "test_value")
	result := utils.Env[string]("TEST_KEY")
	assert.Equal(suite.T(), "test_value", result)

	// Test with default value
	result = utils.Env("NON_EXISTENT_KEY", "default_value")
	assert.Equal(suite.T(), "default_value", result)

	// Test with integer
	viper.Set("TEST_INT", 42)
	intResult := utils.Env[int]("TEST_INT")
	assert.Equal(suite.T(), 42, intResult)

	// Test with default integer
	intResult = utils.Env("NON_EXISTENT_INT", 100)
	assert.Equal(suite.T(), 100, intResult)

	// Test with boolean
	viper.Set("TEST_BOOL", true)
	boolResult := utils.Env[bool]("TEST_BOOL")
	assert.Equal(suite.T(), true, boolResult)

	// Test missing key without default returns zero value
	var zeroInt int
	zeroVal := utils.Env[int]("MISSING_INT")
	assert.Equal(suite.T(), zeroInt, zeroVal)

	// Test type mismatch returns provided default value
	viper.Set("STRING_VALUE", "abc")
	defaultInt := utils.Env[int]("STRING_VALUE", 10)
	assert.Equal(suite.T(), 10, defaultInt)
}

func (suite *EnvironmentTestSuite) TestMustHaveEnv() {
	// Test with value present
	viper.Set("REQUIRED_ENV", "required_value")
	result := utils.MustHaveEnv("REQUIRED_ENV")
	assert.Equal(suite.T(), "required_value", result)

	// Create a temporary .env file for testing
	tempEnvContent := []byte("ENV_FROM_FILE=env_file_value\n")
	err := os.WriteFile(".env", tempEnvContent, 0644)
	assert.NoError(suite.T(), err)
	defer os.Remove(".env")

	// Test reading from .env file
	result = utils.MustHaveEnv("ENV_FROM_FILE")
	assert.Equal(suite.T(), "env_file_value", result)

	// We can't easily test the fatal log scenario in a unit test
	// as it would terminate the program
}

func (suite *EnvironmentTestSuite) TestMustHaveEnvBool() {
	// Test true value
	viper.Set("BOOL_TRUE", "true")
	result := utils.MustHaveEnvBool("BOOL_TRUE")
	assert.True(suite.T(), result)

	// Test false value
	viper.Set("BOOL_FALSE", "false")
	result = utils.MustHaveEnvBool("BOOL_FALSE")
	assert.False(suite.T(), result)
}

func (suite *EnvironmentTestSuite) TestMustHaveEnvInt() {
	// Test valid integer
	viper.Set("INT_VALUE", "42")
	result := utils.MustHaveEnvInt("INT_VALUE")
	assert.Equal(suite.T(), 42, result)

	// Test zero
	viper.Set("ZERO_INT", "0")
	result = utils.MustHaveEnvInt("ZERO_INT")
	assert.Equal(suite.T(), 0, result)

	// Test negative number
	viper.Set("NEG_INT", "-10")
	result = utils.MustHaveEnvInt("NEG_INT")
	assert.Equal(suite.T(), -10, result)

	// We can't easily test the fatal log scenario with invalid int
	// as it would terminate the program
}

func (suite *EnvironmentTestSuite) TestMustHaveEnvMinuteDuration() {
	// Test valid duration
	viper.Set("DURATION_MIN", "5")
	result := utils.MustHaveEnvMinuteDuration("DURATION_MIN")
	assert.Equal(suite.T(), 5*time.Minute, result)

	// Test zero duration
	viper.Set("ZERO_DURATION", "0")
	result = utils.MustHaveEnvMinuteDuration("ZERO_DURATION")
	assert.Equal(suite.T(), 0*time.Minute, result)

	// We can't easily test the fatal log scenario with invalid duration
	// as it would terminate the program
}

func (suite *EnvironmentTestSuite) TestLoadConfig() {
	// Create a temporary config file for testing
	tempDir, err := os.MkdirTemp("", "config_test")
	assert.NoError(suite.T(), err)
	defer os.RemoveAll(tempDir)

	// Create test config file
	configContent := []byte("TEST_CONFIG=config_value\n")
	configPath := tempDir + "/.env"
	err = os.WriteFile(configPath, configContent, 0644)
	assert.NoError(suite.T(), err)

	// Test loading config with default name
	err = utils.LoadConfig(tempDir)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "config_value", viper.GetString("TEST_CONFIG"))

	// Test loading config with custom name
	customConfigContent := []byte("CUSTOM_CONFIG=custom_value\n")
	customConfigPath := tempDir + "/custom.env"
	err = os.WriteFile(customConfigPath, customConfigContent, 0644)
	assert.NoError(suite.T(), err)

	viper.Reset()
	err = utils.LoadConfig(tempDir, "custom")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "custom_value", viper.GetString("CUSTOM_CONFIG"))

	// Test error case - non-existent directory
	viper.Reset()
	err = utils.LoadConfig("/non/existent/directory")
	assert.Error(suite.T(), err)
}

func TestEnvironmentTestSuite(t *testing.T) {
	suite.Run(t, new(EnvironmentTestSuite))
}
