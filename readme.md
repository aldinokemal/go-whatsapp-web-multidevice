## Go Whatsapp API Multi Device Version

### How to use

1. Clone this repo `git clone https://github.com/aldinokemal/go-whatsapp-web-multi-device`
2. run `go run main.go`
3. open `http://localhost:3000`

You can fork or edit this source code !

Current API

| Menu                | Method | URL           | parameter                              | type      |
|---------------------|--------|---------------|----------------------------------------|-----------|
| Login               | GET    | /auth/login   |                                        |           |
| Logout              | GET    | /auth/logout  |                                        |           |
| Send Message (Text) | POST   | /send/message | phone_number (62...), message (string) | form-data |