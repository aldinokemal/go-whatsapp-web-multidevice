package websocket

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"github.com/gofiber/contrib/v3/websocket"
	"github.com/gofiber/fiber/v3"
)

// wsConn is the subset of a websocket connection the hub writes to. Tests
// substitute a fake so a stalled client can be observed without a network.
type wsConn interface {
	WriteMessage(messageType int, data []byte) error
	SetWriteDeadline(t time.Time) error
	Close() error
}

type subscriber struct {
	conn   wsConn
	send   chan []byte
	mu     sync.Mutex
	closed bool
	once   sync.Once
	done   chan struct{} // closed when writeLoop returns
}

// clientQueueSize bounds how many messages may wait for one client. A full
// queue disconnects that client instead of stalling the hub.
var clientQueueSize = 16

// writeTimeout is the deadline for a single client write. A client that
// stops reading is disconnected when the deadline passes.
var writeTimeout = 10 * time.Second

func newSubscriber(conn wsConn) *subscriber {
	size := clientQueueSize
	if size < 1 {
		size = 1
	}
	return &subscriber{conn: conn, send: make(chan []byte, size), done: make(chan struct{})}
}

// trySend enqueues payload without blocking. A full or closed queue reports
// failure so the caller can drop the slow client.
func (s *subscriber) trySend(payload []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	select {
	case s.send <- payload:
		return true
	default:
		return false
	}
}

func (s *subscriber) failSlow() {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.send)
	}
	s.mu.Unlock()
	s.closeConn()
}

func (s *subscriber) closeConn() {
	s.once.Do(func() {
		_ = s.conn.Close()
	})
}

func (s *subscriber) writeLoop() {
	defer close(s.done)
	defer func() {
		s.failSlow()
		Unregister <- s
	}()
	for payload := range s.send {
		timeout := writeTimeout
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		if err := s.conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
			logrus.Println("write deadline error:", err)
			return
		}
		if err := s.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			logrus.Println("write error:", err)
			return
		}
	}
}

type BroadcastMessage struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Result  any    `json:"result"`
}

var (
	clients    = make(map[*subscriber]struct{})
	clientsMu  sync.Mutex
	Register   = make(chan *subscriber)
	Broadcast  = make(chan BroadcastMessage)
	Unregister = make(chan *subscriber)
)

// Publish delivers message to the hub or returns false when ctx ends first.
// A full per-client queue cannot block this send: the hub drops that client
// and keeps broadcasting to the others. Existing callers may also send on
// Broadcast; the hub no longer writes sockets on its own goroutine.
func Publish(ctx context.Context, message BroadcastMessage) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case Broadcast <- message:
		return true
	case <-ctx.Done():
		return false
	}
}

func handleRegister(sub *subscriber) {
	clientsMu.Lock()
	clients[sub] = struct{}{}
	clientsMu.Unlock()
	go sub.writeLoop()
	logrus.Println("connection registered")
}

func handleUnregister(sub *subscriber) {
	clientsMu.Lock()
	delete(clients, sub)
	clientsMu.Unlock()
	logrus.Println("connection unregistered")
}

func broadcastMessage(message BroadcastMessage) {
	marshalMessage, err := json.Marshal(message)
	if err != nil {
		logrus.Println("marshal error:", err)
		return
	}

	clientsMu.Lock()
	subs := make([]*subscriber, 0, len(clients))
	for sub := range clients {
		subs = append(subs, sub)
	}
	clientsMu.Unlock()

	for _, sub := range subs {
		if sub.trySend(marshalMessage) {
			continue
		}
		logrus.Println("disconnecting slow websocket client")
		go sub.failSlow()
	}
}

func RunHub() {
	for {
		select {
		case conn := <-Register:
			handleRegister(conn)

		case conn := <-Unregister:
			handleUnregister(conn)

		case message := <-Broadcast:
			logrus.Println("message received:", message)
			broadcastMessage(message)
		}
	}
}

func RegisterRoutes(app fiber.Router, service domainApp.IAppUsecase) {
	app.Use("/ws", func(c fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return c.SendStatus(fiber.StatusUpgradeRequired)
	})

	app.Get("/ws", func(c fiber.Ctx) error {
		// Reject before the upgrade so an untrusted browser never joins the hub.
		// The upgrader repeats the same allowlist, and non-browser clients that
		// omit Origin stay allowed.
		if !config.BrowserSourceAllowed(c.Host(), c.Get(fiber.HeaderOrigin), c.Get(fiber.HeaderReferer), c.Get("Sec-Fetch-Site")) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"code":    "CROSS_SITE_REQUEST_BLOCKED",
				"message": "browser request origin is not allowed",
			})
		}
		origins := config.TrustedBrowserOrigins(c.Host())
		if len(origins) == 0 {
			// An empty list makes the adapter allow every origin.
			origins = []string{"http://127.0.0.1"}
		}
		return websocket.New(func(conn *websocket.Conn) {
			sub := newSubscriber(conn)
			defer func() {
				sub.failSlow()
				// The adapter recycles conn once this handler returns, so the
				// writer must stop first. failSlow closed the socket, which
				// ends a pending write.
				<-sub.done
				Unregister <- sub
			}()

			Register <- sub

			for {
				messageType, message, err := conn.ReadMessage()
				if err != nil {
					if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
						logrus.Println("read error:", err)
					}
					return
				}

				if messageType == websocket.TextMessage {
					var messageData BroadcastMessage
					if err := json.Unmarshal(message, &messageData); err != nil {
						logrus.Println("unmarshal error:", err)
						return
					}

					if messageData.Code == "FETCH_DEVICES" {
						devices, _ := service.FetchDevices(context.Background())
						Broadcast <- BroadcastMessage{
							Code:    "LIST_DEVICES",
							Message: "Device found",
							Result:  devices,
						}
					}
				} else {
					logrus.Println("unsupported message type:", messageType)
				}
			}
		}, websocket.Config{
			Origins:          origins,
			AllowEmptyOrigin: true,
		})(c)
	})
}
