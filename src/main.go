package main

import (
	"embed"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/cmd"
	"github.com/sirupsen/logrus"
)

//go:embed views/index.html
var embedIndex embed.FS

//go:embed views
var embedViews embed.FS

func main() {
	// Set debug logging by default
	logrus.SetLevel(logrus.DebugLevel)
	cmd.Execute(embedIndex, embedViews)
}
