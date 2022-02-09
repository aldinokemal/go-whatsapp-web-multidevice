## Go Whatsapp API Multi Device Version

### How to use

1. Clone this repo `git clone https://github.com/aldinokemal/go-whatsapp-web-multi-device`
2. run `go run main.go`
3. open `http://localhost:3000`

You can fork or edit this source code !

Current API

| Menu                | URL           | parameter                              | type      |
|---------------------|---------------|----------------------------------------|-----------|
| Login               | /auth/login   |                                        |           |
| Logout              | /auth/logout  |                                        |           |
| Send Message (Text) | /send/message | phone_number (62...), message (string) | form-data |