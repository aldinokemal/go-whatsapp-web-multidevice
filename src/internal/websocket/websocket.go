package websocket

import (
	"context"
	"encoding/json"
	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
	"log"
)

type client struct{} // Add more data to this type if needed
type BroadcastMessage struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Result  any    `json:"result"`
}

var Clients = make(map[*websocket.Conn]client) // Note: although large maps with pointer-like types (e.g. strings) as keys are slow, using pointers themselves as keys is acceptable and fast
var Register = make(chan *websocket.Conn)
var Broadcast = make(chan BroadcastMessage)
var Unregister = make(chan *websocket.Conn)

func RunHub() {
	for {
		select {
		case connection := <-Register:
			Clients[connection] = client{}
			log.Println("connection registered")

		case message := <-Broadcast:
			log.Println("message received:", message)
			marshalMessage, err := json.Marshal(message)
			if err != nil {
				log.Println("write error:", err)
				return
			}

			// Send the message to all clients
			for connection := range Clients {
				if err := connection.WriteMessage(websocket.TextMessage, marshalMessage); err != nil {
					log.Println("write error:", err)

					err := connection.WriteMessage(websocket.CloseMessage, []byte{})
					if err != nil {
						log.Println("write message close error:", err)
						return
					}
					err = connection.Close()
					if err != nil {
						log.Println("close error:", err)
						return
					}
					delete(Clients, connection)
				}
			}

		case connection := <-Unregister:
			// Remove the client from the hub
			delete(Clients, connection)

			log.Println("connection unregistered")
		}
	}
}

func RegisterRoutes(app *fiber.App, service domainApp.IAppService) {
	app.Use("/ws", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) { // Returns true if the client requested upgrade to the WebSocket protocol
			return c.Next()
		}
		return c.SendStatus(fiber.StatusUpgradeRequired)
	})

	app.Get("/ws", websocket.New(func(c *websocket.Conn) {
		// When the function returns, unregister the client and close the connection
		defer func() {
			Unregister <- c
			_ = c.Close()
		}()

		// Register the client
		Register <- c

		for {
			messageType, message, err := c.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Println("read error:", err)
				}
				return // Calls the deferred function, i.e. closes the connection on error
			}

			if messageType == websocket.TextMessage {
				// Broadcast the received message
				var messageData BroadcastMessage
				err := json.Unmarshal(message, &messageData)
				if err != nil {
					log.Println("error unmarshal message:", err)
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
				log.Println("websocket message received of type", messageType)
			}
		}
	}))
}
