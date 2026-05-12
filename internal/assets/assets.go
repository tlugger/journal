// Package assets embeds the blog's templates and static files into the
// binary so the deploy artifact is self-contained — no separate `cp` of
// `templates/` and `static/` to the Pi, no chance of the on-disk copy
// drifting from the binary's expectations.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed templates static
var raw embed.FS

// Templates contains every file under templates/ (e.g. base.html, index.html).
var Templates = mustSub("templates")

// Static contains every file under static/ (base.css, favicon/*).
var Static = mustSub("static")

func mustSub(dir string) fs.FS {
	sub, err := fs.Sub(raw, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
