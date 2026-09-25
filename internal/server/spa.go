package server

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net/http"

	"github.com/gofiber/fiber/v3"
)

// baseHrefTag is the <base> tag in frontend/index.html. The UI builds every
// asset, API, and router URL relative to it, so rewriting its href to the
// configured base path is all it takes to serve the UI under a subpath.
const baseHrefTag = `<base href="/" />`

// httpFSAdapter lets Fiber's static middleware serve the same embedded
// http.FileSystem used to load and rewrite the SPA entrypoint.
type httpFSAdapter struct{ http.FileSystem }

func (a httpFSAdapter) Open(name string) (fs.File, error) {
	return a.FileSystem.Open(name)
}

// loadIndexHTML reads index.html from the embedded UI and sets its <base href>
// to basePath.
func loadIndexHTML(fsys http.FileSystem, basePath string) ([]byte, error) {
	f, err := fsys.Open("/index.html")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("reading index.html: %w", err)
	}
	return renderIndexHTML(raw, basePath)
}

func renderIndexHTML(index []byte, basePath string) ([]byte, error) {
	if !bytes.Contains(index, []byte(baseHrefTag)) {
		return nil, fmt.Errorf("index.html has no %s tag", baseHrefTag)
	}
	tag := `<base href="` + html.EscapeString(basePath) + `" />`
	return bytes.Replace(index, []byte(baseHrefTag), []byte(tag), 1), nil
}

// handleIndex serves the rendered index.html for "/" and every client-side
// route, so deep links load the SPA.
func (s *Server) handleIndex(c fiber.Ctx) error {
	if s.indexHTML == nil {
		return fiber.ErrNotFound
	}
	c.Status(fiber.StatusOK)
	c.Type("html", "utf-8")
	return c.Send(s.indexHTML)
}
