package web

import "embed"

//go:embed index.html app.js style.css themes.js favicon.svg vendor
var FS embed.FS
