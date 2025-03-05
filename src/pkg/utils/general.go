package utils

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // Register GIF format
	"image/jpeg"  // For JPEG encoding
	"image/png"   // For PNG encoding
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

	// If an og:image is found, download it and store its content in ImageThumb
	if meta.Image != "" {
		imageResponse, err := http.Get(meta.Image)
		if err != nil {
			log.Printf("Failed to download image: %v", err)
		} else {
			defer imageResponse.Body.Close()

			// Read image data
			imageData, err := io.ReadAll(imageResponse.Body)
			if err != nil {
				log.Printf("Failed to read image data: %v", err)
			} else {
				meta.ImageThumb = imageData

				// Get image dimensions from the actual image rather than OG tags
				imageReader := bytes.NewReader(imageData)
				img, imgFormat, err := image.Decode(imageReader)
				if err == nil {
					bounds := img.Bounds()
					width := uint32(bounds.Max.X - bounds.Min.X)
					height := uint32(bounds.Max.Y - bounds.Min.Y)

					// Check if image has transparency (alpha channel)
					hasTransparency := false

					// Check for transparency by examining image type and pixels
					switch v := img.(type) {
					case *image.NRGBA:
						// NRGBA format - check alpha values
						for y := bounds.Min.Y; y < bounds.Max.Y && !hasTransparency; y++ {
							for x := bounds.Min.X; x < bounds.Max.X; x++ {
								_, _, _, a := v.At(x, y).RGBA()
								if a < 0xffff {
									hasTransparency = true
									break
								}
							}
						}
					case *image.RGBA:
						// RGBA format - check alpha values
						for y := bounds.Min.Y; y < bounds.Max.Y && !hasTransparency; y++ {
							for x := bounds.Min.X; x < bounds.Max.X; x++ {
								_, _, _, a := v.At(x, y).RGBA()
								if a < 0xffff {
									hasTransparency = true
									break
								}
							}
						}
					case *image.NRGBA64:
						// NRGBA64 format - check alpha values
						for y := bounds.Min.Y; y < bounds.Max.Y && !hasTransparency; y++ {
							for x := bounds.Min.X; x < bounds.Max.X; x++ {
								_, _, _, a := v.At(x, y).RGBA()
								if a < 0xffff {
									hasTransparency = true
									break
								}
							}
						}
					default:
						// For other formats, check if the format typically supports transparency
						hasTransparency = imgFormat == "png" || imgFormat == "gif"
					}

					// If image has transparency, create a new image with white background
					if hasTransparency {
						log.Printf("Image has transparency, setting white background")

						// Create a new RGBA image with white background
						newImg := image.NewRGBA(bounds)
						draw.Draw(newImg, bounds, image.NewUniform(color.White), image.Point{}, draw.Src)

						// Draw the original image on top of the white background
						draw.Draw(newImg, bounds, img, bounds.Min, draw.Over)

						// Convert the new image back to bytes
						var buf bytes.Buffer
						switch imgFormat {
						case "png":
							if err := png.Encode(&buf, newImg); err == nil {
								meta.ImageThumb = buf.Bytes()
							} else {
								log.Printf("Failed to encode PNG image: %v", err)
							}
						case "jpeg", "jpg":
							if err := jpeg.Encode(&buf, newImg, nil); err == nil {
								meta.ImageThumb = buf.Bytes()
							} else {
								log.Printf("Failed to encode JPEG image: %v", err)
							}
						case "gif":
							// Note: Simple conversion to PNG for GIF with transparency
							if err := png.Encode(&buf, newImg); err == nil {
								meta.ImageThumb = buf.Bytes()
							} else {
								log.Printf("Failed to encode GIF as PNG: %v", err)
							}
						default:
							// For other formats, try PNG
							if err := png.Encode(&buf, newImg); err == nil {
								meta.ImageThumb = buf.Bytes()
							} else {
								log.Printf("Failed to encode image as PNG: %v", err)
							}
						}
					}

					// Check if image is square (1:1 ratio)
					if width == height && width <= 200 {
						// For 1:1 ratio, leave width and height as nil
						meta.Width = nil
						meta.Height = nil
					} else {
						meta.Width = &width
						meta.Height = &height
					}

					log.Printf("Image dimensions: %dx%d", width, height)
				} else {
					log.Printf("Failed to decode image to get dimensions: %v", err)

					// Fallback to OG tags if image decoding fails
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

					// Check if the OG tags indicate a 1:1 ratio
					if meta.Width != nil && meta.Height != nil && *meta.Width == *meta.Height {
						meta.Width = nil
						meta.Height = nil
					}
				}
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
