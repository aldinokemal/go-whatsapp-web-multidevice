package utils_test

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type UtilsTestSuite struct {
	suite.Suite
}

func (suite *UtilsTestSuite) TestContainsMention() {
	type args struct {
		message string
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "should success get phone when @ with space",
			args: args{message: "welcome @6289123 ."},
			want: []string{"6289123"},
		},
		{
			name: "should success get phone without suffix space",
			args: args{message: "welcome @6289123."},
			want: []string{"6289123"},
		},
		{
			name: "should success get phone without prefix space",
			args: args{message: "welcome@6289123.@hello:@62891823"},
			want: []string{"6289123", "62891823"},
		},
	}
	for _, tt := range tests {
		suite.T().Run(tt.name, func(t *testing.T) {
			got := utils.ContainsMention(tt.args.message)
			assert.Equal(t, tt.want, got)
		})
	}
}

func (suite *UtilsTestSuite) TestRemoveFile() {
	tempFile, err := os.CreateTemp("", "testfile")
	assert.NoError(suite.T(), err)
	tempFilePath := tempFile.Name()
	tempFile.Close()

	err = utils.RemoveFile(0, tempFilePath)
	assert.NoError(suite.T(), err)
	_, err = os.Stat(tempFilePath)
	assert.True(suite.T(), os.IsNotExist(err))
}

func (suite *UtilsTestSuite) TestCreateFolder() {
	tempDir := "testdir"
	err := utils.CreateFolder(tempDir)
	assert.NoError(suite.T(), err)
	_, err = os.Stat(tempDir)
	assert.NoError(suite.T(), err)
	assert.True(suite.T(), err == nil)
	os.RemoveAll(tempDir)
}

func (suite *UtilsTestSuite) TestPanicIfNeeded() {
	assert.PanicsWithValue(suite.T(), "test error", func() {
		utils.PanicIfNeeded("test error")
	})

	assert.NotPanics(suite.T(), func() {
		utils.PanicIfNeeded(nil)
	})
}

func (suite *UtilsTestSuite) TestStrToFloat64() {
	assert.Equal(suite.T(), 123.45, utils.StrToFloat64("123.45"))
	assert.Equal(suite.T(), 0.0, utils.StrToFloat64("invalid"))
	assert.Equal(suite.T(), 0.0, utils.StrToFloat64(""))
}

func (suite *UtilsTestSuite) TestGetMetaDataFromURL() {
	// Mock HTTP server
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<!DOCTYPE html><html><head><title>Test Title</title><meta name='description' content='Test Description'><meta property='og:image' content='http://example.com/image.jpg'></head><body></body></html>`))
	})
	go http.ListenAndServe(":8080", nil)
	time.Sleep(1 * time.Second) // Allow server to start

	meta := utils.GetMetaDataFromURL("http://localhost:8080")
	assert.Equal(suite.T(), "Test Title", meta.Title)
	assert.Equal(suite.T(), "Test Description", meta.Description)
	assert.Equal(suite.T(), "http://example.com/image.jpg", meta.Image)
}

func (suite *UtilsTestSuite) TestDownloadImageFromURL() {
	// Mock HTTP server
	http.HandleFunc("/image.jpg", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("image data"))
	})
	go http.ListenAndServe(":8081", nil)
	time.Sleep(1 * time.Second) // Allow server to start

	imageData, fileName, err := utils.DownloadImageFromURL("http://localhost:8081/image.jpg")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), []byte("image data"), imageData)
	assert.Equal(suite.T(), "image.jpg", fileName)
}

func TestUtilsTestSuite(t *testing.T) {
	suite.Run(t, new(UtilsTestSuite))
}
