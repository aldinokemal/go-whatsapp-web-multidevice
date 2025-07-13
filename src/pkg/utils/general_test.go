package utils_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
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
	// Use httptest.NewServer to mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<!DOCTYPE html><html><head><title>Test Title</title><meta name='description' content='Test Description'><meta property='og:image' content='http://example.com/image.jpg'></head><body></body></html>`))
	}))
	defer server.Close() // Ensure the server is closed when the test ends

	meta, err := utils.GetMetaDataFromURL(server.URL)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "Test Title", meta.Title)
	assert.Equal(suite.T(), "Test Description", meta.Description)
	assert.Equal(suite.T(), "http://example.com/image.jpg", meta.Image)
}

func (suite *UtilsTestSuite) TestGetMetaDataFromURLEdgeCases() {
	// Test with OG title and Twitter image
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<!DOCTYPE html><html><head><meta property='og:title' content='OG Title'><meta name='twitter:image' content='relative-image.jpg'></head><body></body></html>`))
	}))
	defer server1.Close()

	meta, err := utils.GetMetaDataFromURL(server1.URL)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "OG Title", meta.Title)
	assert.Contains(suite.T(), meta.Image, "relative-image.jpg")

	// Test with empty title falling back to title tag
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<!DOCTYPE html><html><head><title>Fallback Title</title><meta property='og:title' content=''></head><body></body></html>`))
	}))
	defer server2.Close()

	meta, err = utils.GetMetaDataFromURL(server2.URL)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "Fallback Title", meta.Title)

	// Test invalid URL
	_, err = utils.GetMetaDataFromURL("not-a-valid-url")
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "unsupported protocol scheme")

	// Test HTTP error
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer errorServer.Close()

	_, err = utils.GetMetaDataFromURL(errorServer.URL)
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "HTTP request failed")

	// Test malformed HTML
	malformedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><title>Test</title></head><invalid-html>`))
	}))
	defer malformedServer.Close()

	meta, err = utils.GetMetaDataFromURL(malformedServer.URL)
	assert.NoError(suite.T(), err) // Should handle malformed HTML gracefully
	assert.Equal(suite.T(), "Test", meta.Title)

	// Test timeout with slow server
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Second) // Longer than client timeout
		w.Write([]byte("slow response"))
	}))
	defer slowServer.Close()

	_, err = utils.GetMetaDataFromURL(slowServer.URL)
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "deadline exceeded")

	// Test too many redirects
	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.String(), http.StatusFound)
	}))
	defer redirectServer.Close()

	_, err = utils.GetMetaDataFromURL(redirectServer.URL)
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "too many redirects")

	// Test with image that has content type but invalid image data (tests error handling path)
	var imageServerURL string
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/invalid.jpg" {
			// Serve content with image content type but invalid image data
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write([]byte("invalid image data"))
		} else {
			w.Write([]byte(`<!DOCTYPE html><html><head><title>Image Test</title><meta property='og:image' content='` + imageServerURL + `/invalid.jpg'></head><body></body></html>`))
		}
	}))
	imageServerURL = imageServer.URL
	defer imageServer.Close()

	meta, err = utils.GetMetaDataFromURL(imageServer.URL)
	assert.NoError(suite.T(), err) // Should handle invalid image gracefully
	assert.Equal(suite.T(), "Image Test", meta.Title)
	assert.Contains(suite.T(), meta.Image, "/invalid.jpg")
	// Image download may fail but meta should still be extracted
}

func (suite *UtilsTestSuite) TestDownloadImageFromURL() {
	// Use httptest.NewServer to mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/image.jpg" {
			w.Header().Set("Content-Type", "image/jpeg") // Set content type to image
			w.Write([]byte("image data"))
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close() // Ensure the server is closed when the test ends

	imageData, fileName, err := utils.DownloadImageFromURL(server.URL + "/image.jpg")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), []byte("image data"), imageData)
	assert.Equal(suite.T(), "image.jpg", fileName)
}

func (suite *UtilsTestSuite) TestRemoveFileEdgeCases() {
	// Test empty path handling
	err := utils.RemoveFile(0, "")
	assert.NoError(suite.T(), err, "Should handle empty path gracefully")

	// Test multiple files removal
	tempFile1, err := os.CreateTemp("", "testfile1")
	assert.NoError(suite.T(), err)
	tempFile1Path := tempFile1.Name()
	tempFile1.Close()

	tempFile2, err := os.CreateTemp("", "testfile2")
	assert.NoError(suite.T(), err)
	tempFile2Path := tempFile2.Name()
	tempFile2.Close()

	err = utils.RemoveFile(0, tempFile1Path, tempFile2Path)
	assert.NoError(suite.T(), err)

	// Verify both files are removed
	_, err = os.Stat(tempFile1Path)
	assert.True(suite.T(), os.IsNotExist(err))
	_, err = os.Stat(tempFile2Path)
	assert.True(suite.T(), os.IsNotExist(err))

	// Test delay functionality
	tempFile3, err := os.CreateTemp("", "testfile3")
	assert.NoError(suite.T(), err)
	tempFile3Path := tempFile3.Name()
	tempFile3.Close()

	start := time.Now()
	err = utils.RemoveFile(1, tempFile3Path) // 1 second delay
	duration := time.Since(start)
	assert.NoError(suite.T(), err)
	assert.True(suite.T(), duration >= time.Second, "Should respect delay parameter")

	// Test non-existent file error
	err = utils.RemoveFile(0, "/non/existent/file.txt")
	assert.Error(suite.T(), err, "Should return error for non-existent file")
}

func (suite *UtilsTestSuite) TestCreateFolderEdgeCases() {
	// Test nested folder creation
	nestedPath := filepath.Join("test", "nested", "folder")
	err := utils.CreateFolder(nestedPath)
	assert.NoError(suite.T(), err)

	_, err = os.Stat(nestedPath)
	assert.NoError(suite.T(), err)
	os.RemoveAll("test")

	// Test multiple folders creation
	folder1 := "testfolder1"
	folder2 := "testfolder2"
	err = utils.CreateFolder(folder1, folder2)
	assert.NoError(suite.T(), err)

	_, err = os.Stat(folder1)
	assert.NoError(suite.T(), err)
	_, err = os.Stat(folder2)
	assert.NoError(suite.T(), err)

	os.RemoveAll(folder1)
	os.RemoveAll(folder2)

	// Test creating folder that already exists
	existingFolder := "existing"
	err = os.Mkdir(existingFolder, 0755)
	assert.NoError(suite.T(), err)

	err = utils.CreateFolder(existingFolder)
	assert.NoError(suite.T(), err, "Should handle existing folder gracefully")

	os.RemoveAll(existingFolder)
}

func (suite *UtilsTestSuite) TestPanicIfNeededEdgeCases() {
	// Test "record not found" with custom message
	assert.PanicsWithValue(suite.T(), "Custom not found message", func() {
		utils.PanicIfNeeded("record not found", "Custom not found message")
	})

	// Test "record not found" without custom message
	assert.PanicsWithValue(suite.T(), "record not found", func() {
		utils.PanicIfNeeded("record not found")
	})

	// Test other error types
	assert.PanicsWithValue(suite.T(), "some other error", func() {
		utils.PanicIfNeeded("some other error")
	})

	// Test with error interface
	testErr := fmt.Errorf("test error")
	assert.PanicsWithValue(suite.T(), testErr, func() {
		utils.PanicIfNeeded(testErr)
	})
}

func (suite *UtilsTestSuite) TestStrToFloat64EdgeCases() {
	// Test with whitespace
	assert.Equal(suite.T(), 123.45, utils.StrToFloat64("  123.45  "))

	// Test with negative numbers
	assert.Equal(suite.T(), -123.45, utils.StrToFloat64("-123.45"))

	// Test with zero
	assert.Equal(suite.T(), 0.0, utils.StrToFloat64("0"))
	assert.Equal(suite.T(), 0.0, utils.StrToFloat64("0.0"))

	// Test with scientific notation
	assert.Equal(suite.T(), 1.23e2, utils.StrToFloat64("1.23e2"))

	// Test with various invalid inputs
	assert.Equal(suite.T(), 0.0, utils.StrToFloat64("not_a_number"))
	assert.Equal(suite.T(), 0.0, utils.StrToFloat64("123.45.67"))
	assert.Equal(suite.T(), 0.0, utils.StrToFloat64("abc123"))
}

func (suite *UtilsTestSuite) TestDownloadImageFromURLEdgeCases() {
	// Test non-image content type
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("not an image"))
	}))
	defer server.Close()

	_, _, err := utils.DownloadImageFromURL(server.URL)
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "invalid content type")

	// Test HTTP error status
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer errorServer.Close()

	_, _, err = utils.DownloadImageFromURL(errorServer.URL)
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "HTTP request failed")

	// Test unsupported file extension
	extServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/image.gif" {
			w.Header().Set("Content-Type", "image/gif")
			w.Write([]byte("gif data"))
		}
	}))
	defer extServer.Close()

	_, _, err = utils.DownloadImageFromURL(extServer.URL + "/image.gif")
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "unsupported file type")

	// Test valid image extensions
	validExtServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, ".jpg") {
			w.Header().Set("Content-Type", "image/jpeg")
		} else if strings.HasSuffix(path, ".png") {
			w.Header().Set("Content-Type", "image/png")
		} else if strings.HasSuffix(path, ".webp") {
			w.Header().Set("Content-Type", "image/webp")
		}
		w.Write([]byte("valid image data"))
	}))
	defer validExtServer.Close()

	// Test .jpg
	data, filename, err := utils.DownloadImageFromURL(validExtServer.URL + "/test.jpg")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "test.jpg", filename)
	assert.Equal(suite.T(), []byte("valid image data"), data)

	// Test .png
	data, filename, err = utils.DownloadImageFromURL(validExtServer.URL + "/test.png")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "test.png", filename)

	// Test .webp
	data, filename, err = utils.DownloadImageFromURL(validExtServer.URL + "/test.webp")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "test.webp", filename)

	// Test filename extraction with query parameters
	data, filename, err = utils.DownloadImageFromURL(validExtServer.URL + "/test.jpg?v=1&size=large")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "test.jpg", filename)
}

func (suite *UtilsTestSuite) TestDownloadAudioFromURL() {
	// Mock original config values
	origMaxSize := config.WhatsappSettingMaxDownloadSize
	config.WhatsappSettingMaxDownloadSize = 1024 * 1024 // 1MB for testing
	defer func() {
		config.WhatsappSettingMaxDownloadSize = origMaxSize
	}()

	// Test successful audio download
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/test.mp3" {
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Write([]byte("audio data"))
		} else if path == "/test.wav" {
			w.Header().Set("Content-Type", "audio/wav")
			w.Write([]byte("wav audio data"))
		} else if path == "/test.ogg" {
			w.Header().Set("Content-Type", "audio/ogg")
			w.Write([]byte("ogg audio data"))
		} else if path == "/test.m4a" {
			w.Header().Set("Content-Type", "audio/m4a")
			w.Write([]byte("m4a audio data"))
		} else if path == "/large.mp3" {
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Header().Set("Content-Length", "2097152") // 2MB
			w.Write([]byte("large audio"))
		} else if path == "/invalid.mp3" {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("not audio"))
		} else if path == "/no-filename/" {
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Write([]byte("audio without filename"))
		}
	}))
	defer server.Close()

	// Test valid MP3 download
	data, filename, err := utils.DownloadAudioFromURL(server.URL + "/test.mp3")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "test.mp3", filename)
	assert.Equal(suite.T(), []byte("audio data"), data)

	// Test valid WAV download
	data, filename, err = utils.DownloadAudioFromURL(server.URL + "/test.wav")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "test.wav", filename)

	// Test valid OGG download
	data, filename, err = utils.DownloadAudioFromURL(server.URL + "/test.ogg")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "test.ogg", filename)

	// Test valid M4A download
	data, filename, err = utils.DownloadAudioFromURL(server.URL + "/test.m4a")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "test.m4a", filename)

	// Test invalid content type
	_, _, err = utils.DownloadAudioFromURL(server.URL + "/invalid.mp3")
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "invalid content type")

	// Test file too large by content length
	_, _, err = utils.DownloadAudioFromURL(server.URL + "/large.mp3")
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "exceeds maximum allowed size")

	// Test HTTP error
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errorServer.Close()

	_, _, err = utils.DownloadAudioFromURL(errorServer.URL + "/error.mp3")
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "HTTP request failed")

	// Test filename without extension (should generate timestamp-based name)
	data, filename, err = utils.DownloadAudioFromURL(server.URL + "/no-filename/")
	assert.NoError(suite.T(), err)
	assert.Contains(suite.T(), filename, "audio_")
	assert.Equal(suite.T(), []byte("audio without filename"), data)

	// Test URL with query parameters
	data, filename, err = utils.DownloadAudioFromURL(server.URL + "/test.mp3?v=1&quality=high")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "test.mp3", filename)

	// Test invalid URL
	_, _, err = utils.DownloadAudioFromURL("not-a-valid-url")
	assert.Error(suite.T(), err)

	// Test too many redirects
	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.String(), http.StatusFound)
	}))
	defer redirectServer.Close()

	_, _, err = utils.DownloadAudioFromURL(redirectServer.URL + "/redirect.mp3")
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "too many redirects")
}

func (suite *UtilsTestSuite) TestDownloadVideoFromURL() {
	// Mock original config values
	origMaxSize := config.WhatsappSettingMaxDownloadSize
	config.WhatsappSettingMaxDownloadSize = 1024 * 1024 // 1MB for testing
	defer func() {
		config.WhatsappSettingMaxDownloadSize = origMaxSize
	}()

	// Test successful video download
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/test.mp4" {
			w.Header().Set("Content-Type", "video/mp4")
			w.Write([]byte("video data"))
		} else if path == "/test.mkv" {
			w.Header().Set("Content-Type", "video/x-matroska")
			w.Write([]byte("mkv video data"))
		} else if path == "/test.avi" {
			w.Header().Set("Content-Type", "video/avi")
			w.Write([]byte("avi video data"))
		} else if path == "/large.mp4" {
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Content-Length", "2097152") // 2MB
			w.Write([]byte("large video"))
		} else if path == "/invalid.mp4" {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("not video"))
		} else if path == "/no-filename/" {
			w.Header().Set("Content-Type", "video/mp4")
			w.Write([]byte("video without filename"))
		}
	}))
	defer server.Close()

	// Test valid MP4 download
	data, filename, err := utils.DownloadVideoFromURL(server.URL + "/test.mp4")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "test.mp4", filename)
	assert.Equal(suite.T(), []byte("video data"), data)

	// Test valid MKV download
	data, filename, err = utils.DownloadVideoFromURL(server.URL + "/test.mkv")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "test.mkv", filename)

	// Test valid AVI download
	data, filename, err = utils.DownloadVideoFromURL(server.URL + "/test.avi")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "test.avi", filename)

	// Test invalid content type
	_, _, err = utils.DownloadVideoFromURL(server.URL + "/invalid.mp4")
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "invalid content type")

	// Test file too large by content length
	_, _, err = utils.DownloadVideoFromURL(server.URL + "/large.mp4")
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "exceeds maximum allowed size")

	// Test HTTP error
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer errorServer.Close()

	_, _, err = utils.DownloadVideoFromURL(errorServer.URL + "/error.mp4")
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "HTTP request failed")

	// Test filename without extension (should generate timestamp-based name)
	data, filename, err = utils.DownloadVideoFromURL(server.URL + "/no-filename/")
	assert.NoError(suite.T(), err)
	assert.Contains(suite.T(), filename, "video_")
	assert.True(suite.T(), strings.HasSuffix(filename, ".mp4"))
	assert.Equal(suite.T(), []byte("video without filename"), data)

	// Test URL with query parameters
	data, filename, err = utils.DownloadVideoFromURL(server.URL + "/test.mp4?v=1&quality=hd")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "test.mp4", filename)

	// Test invalid URL
	_, _, err = utils.DownloadVideoFromURL("not-a-valid-url")
	assert.Error(suite.T(), err)

	// Test too many redirects
	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.String(), http.StatusFound)
	}))
	defer redirectServer.Close()

	_, _, err = utils.DownloadVideoFromURL(redirectServer.URL + "/redirect.mp4")
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "too many redirects")
}

func TestUtilsTestSuite(t *testing.T) {
	suite.Run(t, new(UtilsTestSuite))
}
