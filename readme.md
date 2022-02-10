## Go Whatsapp API Multi Device Version

### How to use

1. Clone this repo `git clone https://github.com/aldinokemal/go-whatsapp-web-multi-device`
2. run `go run main.go`
3. open `http://localhost:3000`

You can fork or edit this source code !

Current API

| Feature  | Menu                 | Method | URL            | parameter                                                                                       | type        |
|----------|----------------------|--------|----------------|-------------------------------------------------------------------------------------------------|-------------|
| ✅        | Login                | GET    | /app/login     |                                                                                                 |             |
| ✅        | Logout               | GET    | /app/logout    |                                                                                                 |             |
| ✅        | Reconnect            | GET    | /app/reconnect |                                                                                                 |             |
| ❌        | User Info            | GET    | /user/info     | phone_number (string: 62...)                                                                    | querystring |
| ❌        | User Avatar          | GET    | /user/avatar   | phone_number (string: 62...)                                                                    | querystring |
| ✅        | Send Message (Text)  | POST   | /send/message  | phone_number (string: 62...) <br/> message (string)                                             | form-data   |
| ✅        | Send Message (Image) | POST   | /send/message  | phone_number (string: 62...) <br/> caption (string) <br/> image (binary) <br/> view_once (bool) | form-data   |

```
✅ = Available
❌ = Not Available Yet
```

### Mac OS NOTE

Please do this if you have an error (invalid flag in pkg-config --cflags:
-Xpreprocessor) `export CGO_CFLAGS_ALLOW="-Xpreprocessor"`
