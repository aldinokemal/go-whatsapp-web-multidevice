## Go Whatsapp API Multi Device Version

### Required (without docker)

- Mac OS:
    - `brew install vips`
    - `export CGO_CFLAGS_ALLOW="-Xpreprocessor"`
- Linux:
    - `sudo apt update`
    - `sudo apt install libvips-dev`
- Windows (not recomended, prefer using [WSL](https://docs.microsoft.com/en-us/windows/wsl/install)):
    - install vips library, or you can check here https://www.libvips.org/install.html
    - add to [environment variable](https://www.google.com/search?q=windows+add+to+environment+path)

### How to use

#### Basic

1. Clone this repo `git clone https://github.com/aldinokemal/go-whatsapp-web-multi-device`
2. open via cmd/terminal
3. run `cd src`
4. run `go run main.go`
5. open `http://localhost:3000`

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
   2. Windows: `.\whatsapp.exe` or you can double-click it
7. open `http://localhost:3000` in browser

## Production Mode (without config)
- `docker run --publish 3000:3000 --restart=always aldinokemal2104/go-whatsapp-web-multidevice`

You can fork or edit this source code !

### Current API
You can check [docs/openapi.yml](./docs/openapi.yaml) for detail API

| Feature | Menu                    | Method | URL              | Payload                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
|---------|-------------------------|--------|------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| ✅       | Login                   | GET    | /app/login       |                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| ✅       | Logout                  | GET    | /app/logout      |                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |  
| ✅       | Reconnect               | GET    | /app/reconnect   |                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 | 
| ✅       | User Info               | GET    | /user/info       | <table> <thead> <tr> <th>Param</th> <th>Type</th> <th>Type</th> <th>Example</th> </tr></thead> <tbody> <tr> <td>phone</td><td>string</td><td>querystring</td><td>6289685024099</td></tr></tbody></table>                                                                                                                                                                                                                                                                                                                                                        |
| ✅       | User Avatar             | GET    | /user/avatar     | <table> <thead> <tr> <th>Param</th> <th>Type</th> <th>Type</th> <th>Example</th> </tr></thead> <tbody> <tr> <td>phone</td><td>string</td><td>querystring</td><td>6289685024099</td></tr></tbody></table>                                                                                                                                                                                                                                                                                                                                                        |
| ✅       | User My Group List      | GET    | /user/my/groups  |                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| ✅       | User My Privacy Setting | GET    | /user/my/privacy |                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| ✅       | Send Message (Text)     | POST   | /send/message    | <table> <thead> <tr> <th>Param</th> <th>Data Type</th> <th>Type</th> <th>Example</th> </tr></thead> <tbody> <tr> <td>phone</td><td>string</td><td>form-data</td><td>6289685024099</td></tr><tr> <td>message</td><td>string</td><td>form-data</td><td>Hello guys this is testing</td></tr><tr> <td>type</td><td>string (user/group)</td><td>form-data</td><td>user</td></tr></tbody></table>                                                                                                                                                                     |
| ✅       | Send Message (Image)    | POST   | /send/image      | <table> <thead> <tr> <th>Param</th> <th>Type</th> <th>Type</th> <th>Example</th> </tr></thead> <tbody> <tr> <td>phone</td><td>string</td><td>form-data</td><td>6289685024099</td></tr><tr> <td>caption</td><td>string</td><td>form-data</td><td>Hello guys this is caption</td></tr><tr> <td>view_once</td><td>bool</td><td>form-data</td><td>false</td></tr><tr> <td>image</td><td>binary</td><td>form-data</td><td>image/jpg,image/jpeg,image/png</td></tr><tr> <td>type</td><td>string (user/group)</td><td>form-data</td><td>user</td></tr></tbody></table> | 
| ✅       | Send Message (File)     | POST   | /send/file       | <table><thead><tr><th>Param</th><th>Type</th><th>Type</th><th>Example</th></tr></thead><tbody><tr><td>phone</td><td>string</td><td>form-data</td><td>6289685024099</td></tr><tr><td>file</td><td>binary</td><td>form-data</td><td>any (max: 10MB)</td></tr><tr> <td>type</td><td>string (user/group)</td><td>form-data</td><td>user</td></tr></tbody></table>                                                                                                                                                                                                   | 
| ❌       | Send Message (Video)    | POST   | /send/video      | <table><thead><tr><th>Param</th><th>Type</th><th>Type</th><th>Example</th></tr></thead><tbody><tr><td>phone</td><td>string</td><td>form-data</td><td>6289685024099</td></tr><tr><td>video</td><td>binary</td><td>form-data</td><td>mp4/avi/mkv</td></tr><tr> <td>type</td><td>string (user/group)</td><td>form-data</td><td>user</td></tr></tbody></table>                                                                                                                                                                                                      | 

```
✅ = Available
❌ = Not Available Yet
```

### App User Interface

1. Homepage  ![Homepage](https://i.ibb.co/xg6r0BV/Screen-Shot-2022-04-23-at-19-55-56.png)
2. Login  ![Login](https://i.ibb.co/Yp3YJKM/Screen-Shot-2022-02-13-at-12-55-54.png)
3. Send Message ![Send Message](https://i.ibb.co/YcSfvmP/Screen-Shot-2022-02-13-at-12-58-58.png)
4. Send Image ![Send Image](https://i.ibb.co/HDVJZSN/Screen-Shot-2022-02-13-at-12-59-06.png)
5. Send File ![Send File](https://i.ibb.co/XxNnsQ8/Screen-Shot-2022-02-13-at-12-59-14.png)
6. User Info  ![User Info](https://i.ibb.co/BC0mNT7/Screen-Shot-2022-02-13-at-13-00-57.png)
6. User Avatar  ![User Avatar](https://i.ibb.co/TkzPbLZ/Screen-Shot-2022-02-13-at-13-01-39.png)
7. User Privacy ![User My Privacy](https://i.ibb.co/RQcC5m9/Screen-Shot-2022-02-13-at-12-58-47.png)
8. User Group  ![List Group](https://i.ibb.co/jfkgKdG/Screen-Shot-2022-05-12-at-21-12-06.png)

### Mac OS NOTE

- Please do this if you have an error (invalid flag in pkg-config --cflags: -Xpreprocessor)
  `export CGO_CFLAGS_ALLOW="-Xpreprocessor"`
