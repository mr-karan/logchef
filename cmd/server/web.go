package main

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed ui/*
var uiFS embed.FS

// getWebFS returns the embedded frontend files
func getWebFS() http.FileSystem {
	fsys, err := fs.Sub(uiFS, "ui")
	if err != nil {
		panic(err)
	}
	return http.FS(fsys)
}
