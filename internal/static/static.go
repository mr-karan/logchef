package static

import "embed"

//go:embed dist/*
var Static embed.FS
