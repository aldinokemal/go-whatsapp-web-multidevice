## WhatsApp API Multi Device Version

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

- Send WhatsApp message via http API, [docs/openapi.yml](./docs/openapi.yaml) for more details
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
- Webhook Secret

  Our webhook will be sent to you with an HMAC header and a sha256 default key `secret`.<br>
  You may modify this by using the option below:
    - `--webhook-secret="secret"`
- For more command `./main --help`

### Required (without docker)

- Mac OS:
    - `brew install ffmpeg`
    - `export CGO_CFLAGS_ALLOW="-Xpreprocessor"`
- Linux:
    - `sudo apt update`
    - `sudo apt install ffmpeg`
- Windows (not recomended, prefer using [WSL](https://docs.microsoft.com/en-us/windows/wsl/install)):
    - install ffmpeg, download [here](https://www.ffmpeg.org/download.html#build-windows)
    - add to ffmpeg to [environment variable](https://www.google.com/search?q=windows+add+to+environment+path)

### How to use

#### Basic

1. Clone this repo: `git clone https://github.com/aldinokemal/go-whatsapp-web-multidevice`
2. Open the folder that was cloned via cmd/terminal.
3. run `cd src`
4. run `go run main.go`
5. Open `http://localhost:3000`

#### Docker (you don't need to install in required)

1. Clone this repo: `git clone https://github.com/aldinokemal/go-whatsapp-web-multidevice`
2. Open the folder that was cloned via cmd/terminal.
3. run `docker-compose up -d --build`
4. open `http://localhost:3000`

#### Build your own binary

1. Clone this repo `git clone https://github.com/aldinokemal/go-whatsapp-web-multidevice`
2. Open the folder that was cloned via cmd/terminal.
3. run `cd src`
4. run
    1. Linux & MacOS: `go build -o whatsapp`
    2. Windows (CMD / PowerShell): `go build -o whatsapp.exe`
5. run
    1. Linux & MacOS: `./whatsapp`
        1. run `./whatsapp --help` for more detail flags
    2. Windows: `.\whatsapp.exe` or you can double-click it
        1. run `.\whatsapp.exe --help` for more detail flags
6. open `http://localhost:3000` in browser

### Production Mode (docker)

```
docker run --detach --publish=3000:3000 --name=whatsapp --restart=always --volume=$(docker volume create --name=whatsapp):/app/storages aldinokemal2104/go-whatsapp-web-multidevice --autoreply="Dont't reply this message please"
```

### Production Mode (binary)

- download binary from [release](https://github.com/aldinokemal/go-whatsapp-web-multidevice/releases)

You can fork or edit this source code !

### Current API

- [Api Specification Document](https://bump.sh/aldinokemal/doc/go-whatsapp-web-multidevice)
- You can check [docs/openapi.yml](./docs/openapi.yaml) for detail API or paste
  to [SwaggerEditor](https://editor.swagger.io).
- Furthermore you can generate HTTP Client from this API using [openapi-generator](https://openapi-generator.tech/#try)

| Feature | Menu                         | Method | URL                           | 
|---------|------------------------------|--------|-------------------------------|
| ✅       | Login with Scan QR           | GET    | /app/login                    |
| ✅       | Login With Pair Code         | GET    | /app/login-with-code          |
| ✅       | Logout                       | GET    | /app/logout                   |  
| ✅       | Reconnect                    | GET    | /app/reconnect                | 
| ✅       | Devices                      | GET    | /app/devices                  | 
| ✅       | User Info                    | GET    | /user/info                    |
| ✅       | User Avatar                  | GET    | /user/avatar                  |
| ✅       | User My Groups               | GET    | /user/my/groups               |
| ✅       | User My Newsletter           | GET    | /user/my/newsletters          |
| ✅       | User My Privacy Setting      | GET    | /user/my/privacy              |
| ✅       | Send Message                 | POST   | /send/message                 |
| ✅       | Send Image                   | POST   | /send/image                   | 
| ✅       | Send Audio                   | POST   | /send/audio                   | 
| ✅       | Send File                    | POST   | /send/file                    | 
| ✅       | Send Video                   | POST   | /send/video                   | 
| ✅       | Send Contact                 | POST   | /send/contact                 |
| ✅       | Send Link                    | POST   | /send/link                    |
| ✅       | Send Location                | POST   | /send/location                |
| ✅       | Send Poll / Vote             | POST   | /send/poll                    |
| ✅       | Revoke Message               | POST   | /message/:message_id/revoke   |
| ✅       | React Message                | POST   | /message/:message_id/reaction |
| ✅       | Delete Message               | POST   | /message/:message_id/delete   |
| ✅       | Edit Message                 | POST   | /message/:message_id/update   |
| ✅       | Read Message (DM)            | POST   | /message/:message_id/read     |
| ❌       | Star message                 | POST   | /message/:message_id/star     |
| ✅       | Join Group With Link         | POST   | /group/join-with-link         |
| ✅       | Leave Group                  | POST   | /group/leave                  |
| ✅       | Create Group                 | POST   | /group                        |
| ✅       | Add Participants in Group    | POST   | /group/participants           |
| ✅       | Remove Participant in Group  | POST   | /group/participants/remove    |
| ✅       | Promote Participant in Group | POST   | /group/participants/promote   |
| ✅       | Demote Participant in Group  | POST   | /group/participants/demote    |
| ✅       | Unfollow Newsletter          | POST   | /newsletter/unfollow          |

```
✅ = Available
❌ = Not Available Yet
```

### User Interface

| Description        | Image                                                                                    |
|--------------------|------------------------------------------------------------------------------------------|
| Homepage           | ![Homepage](https://i.ibb.co.com/Sy0dHZp/homepage-v4-20.png)                             |
| Login              | ![Login](https://i.ibb.co.com/jkcB15R/login.png?v=1)                                     |
| Login With Code    | ![Login With Code](https://i.ibb.co.com/rdJGvGw/paircode.png)                            |
| Send Message       | ![Send Message](https://i.ibb.co.com/rc3NXMX/send-message.png?v1)                        |
| Send Image         | ![Send Image](https://i.ibb.co.com/BcFL3SD/send-image.png?v1)                            |
| Send File          | ![Send File](https://i.ibb.co.com/f4yxjpp/send-file.png)                                 |
| Send Video         | ![Send Video](https://i.ibb.co.com/PrD3P51/send-video.png)                               |
| Send Contact       | ![Send Contact](https://i.ibb.co.com/4810H7N/send-contact.png)                           |
| Send Location      | ![Send Location](https://i.ibb.co.com/TWsy09G/send-location.png)                         |
| Send Audio         | ![Send Audio](https://i.ibb.co.com/p1wL4wh/Send-Audio.png)                               |
| Send Poll          | ![Send Poll](https://i.ibb.co.com/mq2fGHz/send-poll.png)                                 |
| Revoke Message     | ![Revoke Message](https://i.ibb.co.com/yswhvQY/revoke.png?v1)                            |
| Delete Message     | ![Delete Message](https://i.ibb.co.com/F70SZ84/image.png)                                |
| Reaction Message   | ![Reaction Message](https://i.ibb.co.com/BfHgSHG/react-message.png)                      |
| Edit Message       | ![Edit Message](https://i.ibb.co.com/kXfpqJw/update-message.png)                         |
| User Info          | ![User Info](https://i.ibb.co.com/3zjX6Cz/user-info.png?v=1)                             |
| User Avatar        | ![User Avatar](https://i.ibb.co.com/ZmJZ4ZW/search-avatar.png?v=1)                       |
| My Privacy         | ![My Privacy](https://i.ibb.co.com/Cw1sMQz/my-privacy.png)                               |
| My Group           | ![My Group](https://i.ibb.co.com/WB268Xy/list-group.png)                                 |
| Auto Reply         | ![Auto Reply](https://i.ibb.co.com/D4rTytX/IMG-20220517-162500.jpg)                      |
| Basic Auth Prompt  | ![Basic Auth Prompt](https://i.ibb.co.com/PDjQ92W/Screenshot-2022-11-06-at-14-06-29.png) |
| Manage Participant | ![Manage Participant](https://i.ibb.co.com/ynrN7cr/manage-participant.png)               |
| My Newsletter      | ![List Newsletter](https://i.ibb.co.com/WDg50jJ/image.png)                               |

### Mac OS NOTE

- Please do this if you have an error (invalid flag in pkg-config --cflags: -Xpreprocessor)
  `export CGO_CFLAGS_ALLOW="-Xpreprocessor"`
