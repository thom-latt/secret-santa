package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distEmbed embed.FS

func DistFS() (fs.FS, error) {
	return fs.Sub(distEmbed, "dist")
}
