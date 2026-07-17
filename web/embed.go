package web

import "embed"

//go:embed index.html app.js style.css favicon.svg vendor
var FS embed.FS
