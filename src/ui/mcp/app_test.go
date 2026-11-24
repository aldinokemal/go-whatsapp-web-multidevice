package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/upload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestUploadStore(t *testing.T) *upload.Store {
	t.Helper()
	return upload.NewStore(t.TempDir())
}

type stubAppService struct {
	domainApp.IAppUsecase
	statusDeviceID string
	loggedOut      string
	reconnected    string
	loginDeviceID  string
	pairPhone      string
}

func (s *stubAppService) Status(_ context.Context, deviceID string) (bool, bool, error) {
	s.statusDeviceID = deviceID
	return true, true, nil
}
func (s *stubAppService) Login(_ context.Context, deviceID string) (domainApp.LoginResponse, error) {
	s.loginDeviceID = deviceID
	return domainApp.LoginResponse{ImagePath: "/nonexistent/qr.png", Code: "QRDATA", Duration: 30 * time.Second}, nil
}
func (s *stubAppService) LoginWithCode(_ context.Context, deviceID string, phone string) (string, error) {
	s.loginDeviceID = deviceID
	s.pairPhone = phone
	return "PAIR-CODE", nil
}
func (s *stubAppService) Logout(_ context.Context, deviceID string) error {
	s.loggedOut = deviceID
	return nil
}
func (s *stubAppService) Reconnect(_ context.Context, deviceID string) error {
	s.reconnected = deviceID
	return nil
}

func TestHandleAppDispatch(t *testing.T) {
	t.Run("status", func(t *testing.T) {
		svc := &stubAppService{}
		h := InitMcpApp(svc, newTestUploadStore(t), &stubResolver{})
		res, err := h.handleApp(deviceCtx(), callReq(map[string]any{"action": "status"}))
		require.NoError(t, err)
		assert.False(t, res.IsError)
	})

	t.Run("login_qr survives unreadable image", func(t *testing.T) {
		svc := &stubAppService{}
		h := InitMcpApp(svc, newTestUploadStore(t), &stubResolver{})
		res, err := h.handleApp(deviceCtx(), callReq(map[string]any{"action": "login_qr"}))
		require.NoError(t, err)
		assert.False(t, res.IsError) // falls back to structured result without image
	})

	t.Run("login_code", func(t *testing.T) {
		svc := &stubAppService{}
		h := InitMcpApp(svc, newTestUploadStore(t), &stubResolver{})
		_, err := h.handleApp(deviceCtx(), callReq(map[string]any{"action": "login_code", "phone": " +628123 "}))
		require.NoError(t, err)
		assert.Equal(t, "+628123", svc.pairPhone)
	})

	t.Run("logout", func(t *testing.T) {
		svc := &stubAppService{}
		h := InitMcpApp(svc, newTestUploadStore(t), &stubResolver{})
		res, err := h.handleApp(deviceCtx(), callReq(map[string]any{"action": "logout"}))
		require.NoError(t, err)
		assert.False(t, res.IsError)
	})

	t.Run("reconnect", func(t *testing.T) {
		svc := &stubAppService{}
		h := InitMcpApp(svc, newTestUploadStore(t), &stubResolver{})
		res, err := h.handleApp(deviceCtx(), callReq(map[string]any{"action": "reconnect"}))
		require.NoError(t, err)
		assert.False(t, res.IsError)
	})

	t.Run("create_upload", func(t *testing.T) {
		svc := &stubAppService{}
		h := InitMcpApp(svc, newTestUploadStore(t), &stubResolver{})
		res, err := h.handleApp(deviceCtx(), callReq(map[string]any{"action": "create_upload"}))
		require.NoError(t, err)
		require.False(t, res.IsError)
		structured, ok := res.StructuredContent.(map[string]any)
		require.True(t, ok, "create_upload must return structured content")
		uploadPath, _ := structured["upload_path"].(string)
		fetchPath, _ := structured["fetch_path"].(string)
		assert.True(t, strings.HasPrefix(uploadPath, "/uploads/"))
		// The real format isn't known until after the PUT, and WTF's own
		// image_url downloader insists on a jpg/png/webp-looking URL
		// regardless of the actual Content-Type it also checks - see
		// ui/rest/upload.go's GET route, which ignores this segment
		// entirely and serves by ticket id alone.
		assert.Equal(t, uploadPath+"/file.jpg", fetchPath)
		assert.Equal(t, 600, structured["expires_in_seconds"])
	})

	t.Run("create_upload with no store configured is a tool error", func(t *testing.T) {
		svc := &stubAppService{}
		h := InitMcpApp(svc, nil, &stubResolver{})
		res, err := h.handleApp(deviceCtx(), callReq(map[string]any{"action": "create_upload"}))
		require.NoError(t, err)
		assert.True(t, res.IsError)
	})

	t.Run("no device is a tool error", func(t *testing.T) {
		svc := &stubAppService{}
		h := InitMcpApp(svc, newTestUploadStore(t), &stubResolver{})
		res, err := h.handleApp(context.Background(), callReq(map[string]any{"action": "status"}))
		require.NoError(t, err)
		assert.True(t, res.IsError)
	})
}
