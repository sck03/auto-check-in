package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

func Assets() fs.FS {
	fsys, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
	return fsys
}
