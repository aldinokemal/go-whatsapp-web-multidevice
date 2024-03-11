## Whatsapp API Multi Device Version

![release version](https://img.shields.io/github/v/release/aldinokemal/go-whatsapp-web-multidevice)
<br>
![Build Image](https://github.com/aldinokemal/go-whatsapp-web-multidevice/actions/workflows/build-docker-image.yaml/badge.svg)
<br>
![release windows](https://github.com/aldinokemal/go-whatsapp-web-multidevice/actions/workflows/release-windows.yml/badge.svg)
![release linux](https://github.com/aldinokemal/go-whatsapp-web-multidevice/actions/workflows/release-linux.yml/badge.svg)
![release macos](https://github.com/aldinokemal/go-whatsapp-web-multidevice/actions/workflows/release-mac.yml/badge.svg)

### Support `ARM` Architecture

Now that we support ARM64 for Linux:

- [Release](https://github.com/aldinokemal/go-whatsapp-web-multidevice/releases/latest) for ARM64
- [Docker Image](https://hub.docker.com/r/aldinokemal2104/go-whatsapp-web-multidevice/tags) for ARM64.

### Feature

- Send whatsapp via http API, [docs/openapi.yml](./docs/openapi.yaml) for more details
- Compress image before send
- Compress video before send
- Change OS name become your app (it's the device name when connect via mobile)
    - `--os=Chrome` or `--os=MyApplication`
- Basic Auth (able to add multi credentials)
    - `--basic-auth=kemal:secret,toni:password,userName:secretPassword`, or you can simplify
    - `-b=kemal:secret,toni:password,userName:secretPassword`
- Customizable port and debug mode
    - `--port 8000`
    - `--debug true`
- Auto reply message
    - `--autoreply="Don't reply this message"`
- Webhook for received message
    - `--webhook="http://yourwebhook.site/handler"`, or you can simplify
    - `-w="http://yourwebhook.site/handler"`
- For more command `./main --help`

### Required (without docker)

- Mac OS:
    - `brew install vips`
    - `brew install ffmpeg`
    - `export CGO_CFLAGS_ALLOW="-Xpreprocessor"`
- Linux:
    - `sudo apt update`
    - `sudo apt install libvips-dev`
    - `sudo apt install ffmpeg`
- Windows (not recomended, prefer using [WSL](https://docs.microsoft.com/en-us/windows/wsl/install)):
    - install vips library, or you can check here https://www.libvips.org/install.html
    - install ffmpeg, download [here](https://www.ffmpeg.org/download.html#build-windows)
    - add to vips & ffmpg to [environment variable](https://www.google.com/search?q=windows+add+to+environment+path)

### How to use

#### Basic

1. Clone this repo `git clone https://github.com/aldinokemal/go-whatsapp-web-multi-device`
2. open via cmd/terminal
3. run `go install github.com/markbates/pkger/cmd/pkger@latest`
4. run `cd src`
5. run `pkger && go run main.go`
6. open `http://localhost:3000`
7. run `go run main.go --help` for more detail flags

#### Docker (you don't need to install in required)

1. Clone this repo `git clone https://github.com/aldinokemal/go-whatsapp-web-multi-device`
2. open via cmd/terminal
3. run `docker-compose up -d --build`
4. open `http://localhost:3000`

#### Build your own binary

1. Clone this repo `git clone https://github.com/aldinokemal/go-whatsapp-web-multi-device`
2. open via cmd/terminal
3. run `go install github.com/markbates/pkger/cmd/pkger@latest`
4. run `cd src`
5. run
    1. Linux & MacOS: `pkger && go build -o whatsapp`
    2. Windows (CMD, not PowerShell): `pkger.exe && go build -o whatsapp.exe`
6. run
    1. Linux & MacOS: `./whatsapp`
        1. run `./whatsapp --help` for more detail flags
    2. Windows: `.\whatsapp.exe` or you can double-click it
        1. run `.\whatsapp.exe --help` for more detail flags
7. open `http://localhost:3000` in browser

### Production Mode (docker)

```
docker run --detach --publish=3000:3000 --name=whatsapp --restart=always --volume=$(docker volume create --name=whatsapp):/app/storages aldinokemal2104/go-whatsapp-web-multidevice --autoreply="Dont't reply this message please"
```

### Production Mode (binary)

- download binary from [release](https://github.com/aldinokemal/go-whatsapp-web-multidevice/releases)

You can fork or edit this source code !

### Current API

You can check [docs/openapi.yml](./docs/openapi.yaml) for detail API, furthermore you can generate HTTP Client from this
API using [openapi-generator](https://openapi-generator.tech/#try)

| Feature | Menu                    | Method | URL                         | 
|---------|-------------------------|--------|-----------------------------|
| ✅       | Login                   | GET    | /app/login                  |
| ✅       | Logout                  | GET    | /app/logout                 |  
| ✅       | Reconnect               | GET    | /app/reconnect              | 
| ✅       | User Info               | GET    | /user/info                  |
| ✅       | User Avatar             | GET    | /user/avatar                |
| ✅       | User My Group List      | GET    | /user/my/groups             |
| ✅       | User My Privacy Setting | GET    | /user/my/privacy            |
| ✅       | Send Message            | POST   | /send/message               |
| ✅       | Send Image              | POST   | /send/image                 | 
| ✅       | Send Audio              | POST   | /send/audio                 | 
| ✅       | Send File               | POST   | /send/file                  | 
| ✅       | Send Video              | POST   | /send/video                 | 
| ✅       | Send Contact            | POST   | /send/contact               |
| ✅       | Send Link               | POST   | /send/link                  |
| ✅       | Send Location           | POST   | /send/location              |
| ✅       | Send Poll / Vote        | POST   | /send/poll                  |
| ✅       | Revoke Message          | POST   | /message/:message_id/revoke |
| ✅       | React Message           | POST   | /message/:message_id/react  |
| ✅       | Edit Message            | POST   | /message/:message_id/update |
| ✅       | Join Group With Link    | POST   | /group/join-with-link       |
| ✅       | Leave Group             | POST   | /group/leave                |

```
✅ = Available
❌ = Not Available Yet
```

### App User Interface

1. Homepage ![Homepage](https://i.ibb.co/18f8vCz/homepage.png)
2. Login ![Login](https://i.ibb.co/jkcB15R/login.png)
3. Send Message ![Send Message](https://i.ibb.co/rc3NXMX/send-message.png)
4. Send Image ![Send Image](https://i.ibb.co/BcFL3SD/send-image.png)
5. Send File ![Send File](https://i.ibb.co/f4yxjpp/send-file.png)
6. Send Video ![Send Video](https://i.ibb.co/PrD3P51/send-video.png)
7. Send Contact ![Send Contact](https://i.ibb.co/4810H7N/send-contact.png)
8. Send Location ![Send Location](https://i.ibb.co/TWsy09G/send-location.png)
9. Send Audio ![Send Location](https://i.ibb.co/p1wL4wh/Send-Audio.png)
10. Send Poll ![Send Poll](https://i.ibb.co/mq2fGHz/send-poll.png)
11. Revoke Message ![Revoke Message](https://i.ibb.co/yswhvQY/revoke.png?)
12. Reaction Message ![Revoke Message](https://i.ibb.co/BfHgSHG/react-message.png)
13. Edit Message ![Edit Message](https://i.ibb.co/kXfpqJw/update-message.png)
14. User Info ![User Info](https://i.ibb.co/3zjX6Cz/user-info.png)
15. User Avatar ![User Avatar](https://i.ibb.co/ZmJZ4ZW/search-avatar.png)
16. My Privacy ![My Privacy](https://i.ibb.co/Cw1sMQz/my-privacy.png)
17. My Group ![My Group](https://i.ibb.co/WB268Xy/list-group.png)
18. Auto Reply ![Auto Reply](https://i.ibb.co/D4rTytX/IMG-20220517-162500.jpg)
19. Basic Auth Prompt ![Basic Auth](https://i.ibb.co/PDjQ92W/Screenshot-2022-11-06-at-14-06-29.png)

### Mac OS NOTE

- Please do this if you have an error (invalid flag in pkg-config --cflags: -Xpreprocessor)
  `export CGO_CFLAGS_ALLOW="-Xpreprocessor"`
