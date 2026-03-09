package webui

import (
	"embed"
	"io/fs"
)

// Assets contains the embedded HTML templates and browser assets served by Cheapspace.
//
//go:embed templates/*.html static/*
var Assets embed.FS

func Templates() fs.FS {
	sub, _ := fs.Sub(Assets, "templates")
	return sub
}

func Static() fs.FS {
	sub, _ := fs.Sub(Assets, "static")
	return sub
}
