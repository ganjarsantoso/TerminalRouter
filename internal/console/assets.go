package console

import (
	"embed"
	"io/fs"
)

//go:embed assets/dist
var distFS embed.FS

// embeddedFS adapts the go:embed FS to the embedFS interface.
type embeddedFS struct {
	root fs.FS
}

func (e embeddedFS) Open(name string) (fs.File, error) {
	return e.root.Open(name)
}

func init() {
	if sub, err := fs.Sub(distFS, "assets/dist"); err == nil {
		assetsFS = embeddedFS{root: sub}
	}
}
