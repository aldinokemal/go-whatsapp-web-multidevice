package helpers

import (
	"context"
	"mime/multipart"
	"os"
	"time"

	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
)

func SetAutoConnectAfterBooting(service domainApp.IAppUsecase) {
	time.Sleep(2 * time.Second)
	_ = service.Reconnect(context.Background())
}

func SetAutoReconnectChecking(cli *whatsmeow.Client) {
	// Run every 5 minutes to check if the connection is still alive, if not, reconnect
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			if !cli.IsConnected() {
				_ = cli.Connect()
			}
		}
	}()
}

func MultipartFormFileHeaderToBytes(fileHeader *multipart.FileHeader) []byte {
	file, _ := fileHeader.Open()
	defer file.Close()

	fileBytes := make([]byte, fileHeader.Size)
	_, _ = file.Read(fileBytes)

	return fileBytes
}

// QRTimeoutMonitor monitors for successful login within the specified timeout
// Returns true if login successful, false if timeout occurred
func QRTimeoutMonitor(ctx context.Context, cli *whatsmeow.Client, timeout time.Duration) bool {
	logrus.Infof("Starting QR code timeout monitor for %v", timeout)

	// Create a ticker to check status every 2 seconds
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Create timeout channel
	timeoutChan := time.After(timeout)

	for {
		select {
		case <-ctx.Done():
			logrus.Info("QR timeout monitor cancelled due to context cancellation")
			return false

		case <-timeoutChan:
			logrus.Warn("QR code timeout reached - user did not scan and login within the time limit")
			return false

		case <-ticker.C:
			if cli != nil && cli.IsConnected() && cli.IsLoggedIn() {
				logrus.Info("Login successful - QR code was scanned and authenticated")
				return true
			}
			// Continue monitoring if not logged in yet
		}
	}
}

// CheckInitialConnectionStatus checks if the device is already connected and logged in
func CheckInitialConnectionStatus(cli *whatsmeow.Client) (isConnected bool, isLoggedIn bool, deviceID string) {
	if cli == nil {
		logrus.Warn("WhatsApp client is not initialized")
		return false, false, ""
	}

	isConnected = cli.IsConnected()
	isLoggedIn = cli.IsLoggedIn()

	if cli.Store != nil && cli.Store.ID != nil {
		deviceID = cli.Store.ID.String()
	}

	logrus.Infof("Initial connection status - Connected: %v, Logged In: %v, Device ID: %s",
		isConnected, isLoggedIn, deviceID)

	return isConnected, isLoggedIn, deviceID
}

// GracefulShutdown performs a graceful shutdown of the application
func GracefulShutdown(reason string) {
	logrus.Warnf("Initiating graceful shutdown: %s", reason)
	logrus.Info("Application will exit now. Please restart to try again.")

	// Give some time for log messages to be written
	time.Sleep(1 * time.Second)

	// Exit with status code 1 to indicate unsuccessful termination
	os.Exit(1)
}

// StartHealthCheckMonitor starts a periodic health check that monitors login status
// and implements QR timeout when device is not logged in
// It uses a function to get the current client to handle reinitializations
func StartHealthCheckMonitor(getClientFunc func() *whatsmeow.Client) {
	logrus.Info("Starting health check monitor for QR timeout management")

	go func() {
		// Track when we first detect "not logged in" state
		var notLoggedInSince *time.Time
		checkInterval := 5 * time.Second
		timeout := 2 * time.Minute

		ticker := time.NewTicker(checkInterval)
		defer ticker.Stop()

		for range ticker.C {
			// Get the current client instance (may have been reinitialized)
			cli := getClientFunc()
			if cli == nil {
				continue
			}

			isConnected := cli.IsConnected()
			isLoggedIn := cli.IsLoggedIn()

			// Log health check status for monitoring
			logrus.Infof("Health check: Connected=%v, LoggedIn=%v", isConnected, isLoggedIn)

			if isLoggedIn {
				// User is logged in, reset timeout tracking
				if notLoggedInSince != nil {
					logrus.Info("User logged in - QR timeout cancelled")
					notLoggedInSince = nil
				}
				continue
			}

			// User is NOT logged in
			if notLoggedInSince == nil {
				// First time detecting not logged in
				logrus.Warn("Device not logged in - starting 2-minute QR timeout")
				now := time.Now()
				notLoggedInSince = &now
			} else {
				// Check if timeout has expired
				elapsed := time.Since(*notLoggedInSince)
				remaining := timeout - elapsed

				if remaining <= 0 {
					logrus.Error("QR timeout expired - device not logged in for 2 minutes")
					logrus.Error("Application will exit to prevent hanging sessions")
					logrus.Info("Please restart the application and scan QR code within 2 minutes")

					// Disconnect and shutdown
					if cli != nil {
						cli.Disconnect()
					}
					GracefulShutdown("QR timeout - no login detected")
				} else if remaining <= 30*time.Second && int(remaining.Seconds())%10 == 0 {
					// Warn in the last 30 seconds
					logrus.Warnf("QR timeout in %v - please scan QR code", remaining.Round(time.Second))
				}
			}
		}
	}()
}
