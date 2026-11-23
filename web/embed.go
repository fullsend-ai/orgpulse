// Package web embeds the dashboard's static frontend assets.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:patternfly/build
var pfEmbedded embed.FS

// StaticFS is the embedded PatternFly frontend, rooted at "patternfly/build".
var StaticFS fs.FS = mustSub(pfEmbedded, "patternfly/build")

func mustSub(fsys embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
