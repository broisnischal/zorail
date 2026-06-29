package api

import "embed"

// webFS holds the bundled web UI. `go:embed` bakes these files into the binary
// at compile time, so the same single binary (and the same Dockerfile) serves
// both the JSON API and the dashboard — no separate frontend container.
//
//go:embed all:web
var webFS embed.FS
