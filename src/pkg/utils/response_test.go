package utils_test

import (
	"encoding/json"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type ResponseTestSuite struct {
	suite.Suite
}

func (suite *ResponseTestSuite) TestResponseDataStruct() {
	// Test creating ResponseData with all fields
	responseData := utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Operation completed successfully",
		Results: map[string]any{
			"data":  "test data",
			"count": 10,
		},
	}

	assert.Equal(suite.T(), 200, responseData.Status)
	assert.Equal(suite.T(), "SUCCESS", responseData.Code)
	assert.Equal(suite.T(), "Operation completed successfully", responseData.Message)
	assert.NotNil(suite.T(), responseData.Results)
}

func (suite *ResponseTestSuite) TestResponseDataJSON() {
	// Test JSON marshaling
	responseData := utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Test message",
		Results: "test results",
	}

	jsonData, err := json.Marshal(responseData)
	assert.NoError(suite.T(), err)

	// Status field should be omitted from JSON (json:"-" tag)
	var result map[string]any
	err = json.Unmarshal(jsonData, &result)
	assert.NoError(suite.T(), err)

	// Status should not be in JSON
	_, statusExists := result["status"]
	assert.False(suite.T(), statusExists, "Status field should be omitted from JSON")

	// Other fields should be present
	assert.Equal(suite.T(), "SUCCESS", result["code"])
	assert.Equal(suite.T(), "Test message", result["message"])
	assert.Equal(suite.T(), "test results", result["results"])
}

func (suite *ResponseTestSuite) TestResponseDataWithNilResults() {
	// Test with nil Results (should be omitted from JSON)
	responseData := utils.ResponseData{
		Status:  404,
		Code:    "NOT_FOUND",
		Message: "Resource not found",
		Results: nil,
	}

	jsonData, err := json.Marshal(responseData)
	assert.NoError(suite.T(), err)

	var result map[string]any
	err = json.Unmarshal(jsonData, &result)
	assert.NoError(suite.T(), err)

	// Results should be omitted when nil (omitempty tag)
	_, resultsExists := result["results"]
	assert.False(suite.T(), resultsExists, "Results field should be omitted when nil")

	assert.Equal(suite.T(), "NOT_FOUND", result["code"])
	assert.Equal(suite.T(), "Resource not found", result["message"])
}

func (suite *ResponseTestSuite) TestResponseDataWithEmptyResults() {
	// Test with empty Results
	responseData := utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success with empty results",
		Results: "",
	}

	jsonData, err := json.Marshal(responseData)
	assert.NoError(suite.T(), err)

	var result map[string]any
	err = json.Unmarshal(jsonData, &result)
	assert.NoError(suite.T(), err)

	// Results should be present even if empty string
	results, resultsExists := result["results"]
	assert.True(suite.T(), resultsExists, "Results field should be present for empty string")
	assert.Equal(suite.T(), "", results)
}

func (suite *ResponseTestSuite) TestResponseDataJSONUnmarshaling() {
	// Test JSON unmarshaling
	jsonStr := `{
		"code": "ERROR",
		"message": "Something went wrong",
		"results": {
			"error_details": "detailed error message"
		}
	}`

	var responseData utils.ResponseData
	err := json.Unmarshal([]byte(jsonStr), &responseData)
	assert.NoError(suite.T(), err)

	assert.Equal(suite.T(), 0, responseData.Status) // Default value since not in JSON
	assert.Equal(suite.T(), "ERROR", responseData.Code)
	assert.Equal(suite.T(), "Something went wrong", responseData.Message)
	assert.NotNil(suite.T(), responseData.Results)

	// Check the Results content
	results, ok := responseData.Results.(map[string]any)
	assert.True(suite.T(), ok)
	assert.Equal(suite.T(), "detailed error message", results["error_details"])
}

func (suite *ResponseTestSuite) TestResponseDataZeroValues() {
	// Test zero values
	var responseData utils.ResponseData

	assert.Equal(suite.T(), 0, responseData.Status)
	assert.Equal(suite.T(), "", responseData.Code)
	assert.Equal(suite.T(), "", responseData.Message)
	assert.Nil(suite.T(), responseData.Results)
}

func TestResponseTestSuite(t *testing.T) {
	suite.Run(t, new(ResponseTestSuite))
}
