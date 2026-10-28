package websocket

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	go RunHub()
	m.Run()
}

func TestWebsocketRejectsUntrustedOrigin(t *testing.T) {
	app := fiber.New()
	RegisterRoutes(app, nil)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		_ = ln.Close()
	})
	go func() {
		_ = app.Listener(ln, fiber.ListenConfig{
			DisableStartupMessage: true,
			GracefulContext:       ctx,
			ShutdownTimeout:       time.Second,
		})
	}()

	endpoint := "ws://" + ln.Addr().String() + "/ws"
	dialer := websocket.Dialer{HandshakeTimeout: time.Second}
	var untrusted *websocket.Conn
	var resp *http.Response
	deadline := time.Now().Add(2 * time.Second)
	for {
		untrusted, resp, err = dialer.Dial(endpoint, http.Header{"Origin": []string{"https://evil.example"}})
		if err == nil || resp != nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if untrusted != nil {
		_ = untrusted.Close()
	}
	require.Error(t, err)
	require.NotNil(t, resp)
	require.Equal(t, fiber.StatusForbidden, resp.StatusCode)
	_ = resp.Body.Close()

	trustedOrigin := "http://" + ln.Addr().String()
	conn, resp, err := dialer.Dial(endpoint, http.Header{"Origin": []string{trustedOrigin}})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	require.NoError(t, err)
	require.NotNil(t, conn)
	_ = conn.Close()

	missing, resp, err := dialer.Dial(endpoint, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	require.NoError(t, err)
	require.NotNil(t, missing)
	_ = missing.Close()
}
