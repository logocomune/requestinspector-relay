package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHandlerAppliesStaticAssetHeaders(t *testing.T) {
	handler, err := Handler()
	if err != nil {
		t.Fatal(err)
	}
	immutable := firstImmutableAsset(t)
	tests := []struct {
		name        string
		path        string
		cache       string
		contentType string
	}{
		{name: "navigation", path: "/", cache: "no-cache", contentType: "text/html"},
		{name: "settings navigation", path: "/settings", cache: "no-cache", contentType: "text/html"},
		{name: "about navigation", path: "/about", cache: "no-cache", contentType: "text/html"},
		{name: "credits navigation", path: "/credits", cache: "no-cache", contentType: "text/html"},
		{name: "manifest", path: "/manifest.webmanifest", cache: "no-cache", contentType: "application/manifest+json"},
		{name: "service worker", path: "/service-worker.js", cache: "no-cache", contentType: "text/javascript"},
		{name: "hashed asset", path: immutable, cache: "public, max-age=31536000, immutable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d", response.Code)
			}
			if got := response.Header().Get("Cache-Control"); got != test.cache {
				t.Fatalf("Cache-Control = %q, want %q", got, test.cache)
			}
			if test.contentType != "" && !contentTypeMatches(response.Header().Get("Content-Type"), test.contentType) {
				t.Fatalf("Content-Type = %q, want %q", response.Header().Get("Content-Type"), test.contentType)
			}
		})
	}
}

func contentTypeMatches(got, want string) bool {
	if want == "text/javascript" {
		return strings.HasPrefix(got, "text/javascript") || strings.HasPrefix(got, "application/javascript")
	}
	return strings.HasPrefix(got, want)
}

func TestHandlerLeavesUnknownNavigationMissing(t *testing.T) {
	handler, err := Handler()
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/unknown-route", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func FuzzHandlerNavigation(f *testing.F) {
	for _, path := range []string{"/", "/settings", "/about", "/credits", "/../settings", "/_app/version.json", ""} {
		f.Add(path)
	}
	handler, err := Handler()
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, path string) {
		request := &http.Request{Method: http.MethodGet, URL: &url.URL{Path: path}, Header: make(http.Header)}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code < 100 || response.Code > 599 {
			t.Fatalf("invalid status %d", response.Code)
		}
	})
}

func firstImmutableAsset(t *testing.T) string {
	t.Helper()
	var path string
	err := fs.WalkDir(assets, "dist/_app/immutable", func(candidate string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if path == "" && !entry.IsDir() {
			path = strings.TrimPrefix(candidate, "dist")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("embedded immutable asset not found")
	}
	return path
}
