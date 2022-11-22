package helpers

import (
	"encoding/json"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
	"go.mau.fi/whatsmeow"
	"log"
)

type client struct{} // Add more data to this type if needed
type WsBroadcastMessage struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

var WsClients = make(map[*websocket.Conn]client) // Note: although large maps with pointer-like types (e.g. strings) as keys are slow, using pointers themselves as keys is acceptable and fast
var WsRegister = make(chan *websocket.Conn)
var WsBroadcast = make(chan WsBroadcastMessage)
var WsUnregister = make(chan *websocket.Conn)

func WsRunHub() {
	for {
		select {
		case connection := <-WsRegister:
			WsClients[connection] = client{}
			log.Println("connection registered")

		case message := <-WsBroadcast:
			log.Println("message received:", message)
			marshalMessage, err := json.Marshal(message)
			if err != nil {
				log.Println("write error:", err)
				return
			}

			// Send the message to all clients
			for connection := range WsClients {
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
					delete(WsClients, connection)
				}
			}

		case connection := <-WsUnregister:
			// Remove the client from the hub
			delete(WsClients, connection)

			log.Println("connection unregistered")
		}
	}
}

func WsRegisterRoutes(app *fiber.App, cli *whatsmeow.Client) {
	app.Use("/ws", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) { // Returns true if the client requested upgrade to the WebSocket protocol
			return c.Next()
		}
		return c.SendStatus(fiber.StatusUpgradeRequired)
	})

	app.Get("/ws", websocket.New(func(c *websocket.Conn) {
		// When the function returns, unregister the client and close the connection
		defer func() {
			WsUnregister <- c
			_ = c.Close()
		}()

		// Register the client
		WsRegister <- c

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
				var messageData WsBroadcastMessage
				err := json.Unmarshal(message, &messageData)
				if err != nil {
					log.Println("error unmarshal message:", err)
					return
				}
				WsBroadcast <- messageData
			} else {
				log.Println("websocket message received of type", messageType)
			}
		}
	}))
}
