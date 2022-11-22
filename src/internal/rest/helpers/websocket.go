package helpers

import (
	"encoding/json"
	"github.com/gofiber/websocket/v2"
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
