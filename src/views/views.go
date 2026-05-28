package views

import "embed"

//go:embed index.html
var EmbedIndex embed.FS

//go:embed *
var EmbedViews embed.FS
