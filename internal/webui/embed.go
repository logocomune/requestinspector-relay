package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed dist dist/_app
var assets embed.FS

func Handler() (http.Handler, error) {
	directory, err := fs.Sub(assets, "dist")
	if err != nil {
		return nil, err
	}
	files := http.FileServer(http.FS(directory))
	return cacheHeaders(navigationFiles(directory, files)), nil
}

func navigationFiles(directory fs.FS, files http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		path := strings.TrimPrefix(request.URL.Path, "/")
		if path != "" && !strings.Contains(path, ".") {
			if _, err := fs.Stat(directory, path+".html"); err == nil {
				clone := request.Clone(request.Context())
				clone.URL.Path = "/" + path + ".html"
				files.ServeHTTP(writer, clone)
				return
			}
		}
		files.ServeHTTP(writer, request)
	})
}

func cacheHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		path := request.URL.Path
		switch {
		case strings.HasPrefix(path, "/_app/immutable/"):
			writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		case path == "/manifest.webmanifest":
			writer.Header().Set("Content-Type", "application/manifest+json")
			writer.Header().Set("Cache-Control", "no-cache")
		default:
			writer.Header().Set("Cache-Control", "no-cache")
		}
		next.ServeHTTP(writer, request)
	})
}
