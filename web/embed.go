// Package web holds the viewer's static files, embedded into the binary.
package web

import "embed"

//go:embed index.html viewer.js
var FS embed.FS
