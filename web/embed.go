// Package web embeds the built React app (web/dist) into the mechon binary.
// Build it with `make web` first; without it the panel serves a short notice instead of the UI.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
