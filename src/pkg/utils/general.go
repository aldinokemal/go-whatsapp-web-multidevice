package utils

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
)

// RemoveFile is removing file with delay
func RemoveFile(delaySecond int, paths ...string) error {
	if delaySecond > 0 {
		time.Sleep(time.Duration(delaySecond) * time.Second)
	}

	for _, path := range paths {
		if path != "" {
			err := os.Remove(path)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// CreateFolder create new folder and sub folder if not exist
func CreateFolder(folderPath ...string) error {
	for _, folder := range folderPath {
		newFolder := filepath.Join(".", folder)
		err := os.MkdirAll(newFolder, os.ModePerm)
		if err != nil {
			return err
		}
	}
	return nil
}

// PanicIfNeeded is panic if error is not nil
func PanicIfNeeded(err any, message ...string) {
	if err != nil {
		if fmt.Sprintf("%s", err) == "record not found" && len(message) > 0 {
			panic(message[0])
		} else {
			panic(err)
		}
	}
}

func StrToFloat64(text string) float64 {
	var result float64
	if text != "" {
		result, _ = strconv.ParseFloat(strings.TrimSpace(text), 64)
	}
	return result
}

type Metadata struct {
	Title       string
	Description string
	Image       string
	ImageThumb  []byte
	Height      *uint32
	Width       *uint32
}

func GetMetaDataFromURL(url string) (meta Metadata, err error) {
	// Send an HTTP GET request to the website
	response, err := http.Get(url)
	if err != nil {
		return meta, err
	}
	defer response.Body.Close()

	// Parse the HTML document
	document, err := goquery.NewDocumentFromReader(response.Body)
	if err != nil {
		return meta, err
	}

	document.Find("meta[name='description']").Each(func(index int, element *goquery.Selection) {
		meta.Description, _ = element.Attr("content")
	})

	// find title
	document.Find("title").Each(func(index int, element *goquery.Selection) {
		meta.Title = element.Text()
	})

	document.Find("meta[property='og:image']").Each(func(index int, element *goquery.Selection) {
		meta.Image, _ = element.Attr("content")
	})

	document.Find("meta[property='og:image:width']").Each(func(index int, element *goquery.Selection) {
		if content, exists := element.Attr("content"); exists {
			width, _ := strconv.Atoi(content)
			widthUint32 := uint32(width)
			meta.Width = &widthUint32
		}
	})

	document.Find("meta[property='og:image:height']").Each(func(index int, element *goquery.Selection) {
		if content, exists := element.Attr("content"); exists {
			height, _ := strconv.Atoi(content)
			heightUint32 := uint32(height)
			meta.Height = &heightUint32
		}
	})

	// If an og:image is found, download it and store its content in ImageThumb
	if meta.Image != "" {
		imageResponse, err := http.Get(meta.Image)
		if err != nil {
			log.Printf("Failed to download image: %v", err)
		} else {
			defer imageResponse.Body.Close()
			imageData, err := io.ReadAll(imageResponse.Body)
			if err != nil {
				log.Printf("Failed to read image data: %v", err)
			} else {
				meta.ImageThumb = imageData
			}
		}
	}

	return meta, nil
}

// ContainsMention is checking if message contains mention, then return only mention without @
func ContainsMention(message string) []string {
	// Regular expression to find all phone numbers after the @ symbol
	re := regexp.MustCompile(`@(\d+)`)
	matches := re.FindAllStringSubmatch(message, -1)

	var phoneNumbers []string
	// Loop through the matches and extract the phone numbers
	for _, match := range matches {
		if len(match) > 1 {
			phoneNumbers = append(phoneNumbers, match[1])
		}
	}
	return phoneNumbers
}

func DownloadImageFromURL(url string) ([]byte, string, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	response, err := client.Get(url)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", fmt.Errorf("invalid content type: %s", contentType)
	}
	// Check content length if available
	if contentLength := response.ContentLength; contentLength > int64(config.WhatsappSettingMaxImageSize) {
		return nil, "", fmt.Errorf("image size %d exceeds maximum allowed size %d", contentLength, config.WhatsappSettingMaxImageSize)
	}
	// Limit the size from config
	reader := io.LimitReader(response.Body, int64(config.WhatsappSettingMaxImageSize))
	// Extract the file name from the URL and remove query parameters if present
	segments := strings.Split(url, "/")
	fileName := segments[len(segments)-1]
	fileName = strings.Split(fileName, "?")[0]
	// Check if the file extension is supported
	allowedExtensions := map[string]bool{
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".webp": true,
	}
	extension := strings.ToLower(filepath.Ext(fileName))
	if !allowedExtensions[extension] {
		return nil, "", fmt.Errorf("unsupported file type: %s", extension)
	}
	imageData, err := io.ReadAll(reader)
	if err != nil {
		return nil, "", err
	}
	return imageData, fileName, nil
}
