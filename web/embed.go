// Package web embeds the compiled Svelte applications. The dist directory is
// produced by `npm run build` in the web folder and committed for reproducible
// single-binary builds.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the embedded frontend filesystem rooted at dist.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
