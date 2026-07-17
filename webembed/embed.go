// Package webembed embeds the built Vite SPA (web/dist, copied to dist/ here
// at build time) so the Go binary can serve the frontend with no external
// files. A minimal placeholder dist/index.html is committed so `go build`
// works from a fresh clone; the real assets are produced by `npm run build`
// and copied over dist/ during the Docker/CI build.
package webembed

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the embedded SPA build rooted at the dist directory.
func FS() fs.FS {
	f, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return f
}
