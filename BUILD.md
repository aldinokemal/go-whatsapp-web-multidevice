# Building the Project

This guide explains how to build the project for different platforms.

## Prerequisites

-   Go 1.22 or later
-   Git

## Build Commands

To build the project, navigate to the project root directory and verify the `src` directory exists.

### macOS (Apple Silicon - arm64)

```bash
cd src
GOOS=darwin GOARCH=arm64 go build -o ../bin/whatsapp-api-macos-arm64 ./main.go
```

### macOS (Intel - amd64)

```bash
cd src
GOOS=darwin GOARCH=amd64 go build -o ../bin/whatsapp-api-macos-amd64 ./main.go
```

### Linux (amd64)

```bash
cd src
GOOS=linux GOARCH=amd64 go build -o ../bin/whatsapp-api-linux-amd64 ./main.go
```

### Windows (amd64)

```bash
cd src
GOOS=windows GOARCH=amd64 go build -o ../bin/whatsapp-api-windows-amd64.exe ./main.go
```

## Running the Application

After building, you can run the binary from the `bin` directory:

```bash
./bin/whatsapp-api-macos-arm64
```

Make sure you have the necessary configuration files (like `.env`) in the directory where you run the binary, or configured appropriately.
