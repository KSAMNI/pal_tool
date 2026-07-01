package frontend

import (
	"embed"
	"io/fs"
)

//go:embed dist fallback
var embedded embed.FS

func Dist() fs.FS {
	dist, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
	return dist
}

func FallbackIndex() []byte {
	data, err := embedded.ReadFile("fallback/index.html")
	if err != nil {
		panic(err)
	}
	return data
}
