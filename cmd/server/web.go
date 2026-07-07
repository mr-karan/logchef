package main

import (
	"embed"
	"io/fs"
	"net/http"
)

// all:ui embeds the built frontend, including the committed .placeholder so a
// fresh checkout (CI, or `go build` before `just build`) compiles without the
// frontend dist present. `just build` overwrites ui/ with the real assets.
//
//go:embed all:ui
var uiFS embed.FS

// getWebFS returns the embedded frontend files
func getWebFS() http.FileSystem {
	fsys, err := fs.Sub(uiFS, "ui")
	if err != nil {
		panic(err)
	}
	return http.FS(fsys)
}
