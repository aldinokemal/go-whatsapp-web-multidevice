package main

import (
	"embed"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/cmd"
)

//go:embed views/index.html
var embedViews embed.FS

//go:embed views
var embedViewsComponent embed.FS

func main() {
	cmd.Execute(embedViews, embedViewsComponent)
}
