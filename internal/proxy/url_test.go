package proxy

import (
	"net/url"
	"strings"
	"testing"
	"testing/quick"
)

func TestComposeURL(t *testing.T) {
	tests := []struct {
		name   string
		target string
		path   string
		query  string
		want   string
	}{
		{name: "prefix", target: "https://api.example/service", path: "/orders", query: "id=7&id=8", want: "https://api.example/service/orders?id=7&id=8"},
		{name: "escaped", target: "http://api.example/base%2Fv1/", path: "/a%2Fb/%E2%82%AC", want: "http://api.example/base%2Fv1/a%2Fb/%E2%82%AC"},
		{name: "root", target: "http://api.example/", path: "/", want: "http://api.example/"},
		{name: "empty incoming", target: "http://api.example/base", want: "http://api.example/base/"},
		{name: "case-insensitive scheme", target: "HTTPS://api.example/base", path: "/path", want: "https://api.example/base/path"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := composeURL(test.target, test.path, test.query)
			if err != nil || got.String() != test.want {
				t.Fatalf("composeURL() = %v, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestComposeURLRejectsAmbiguousTargetsAndPaths(t *testing.T) {
	tests := []struct{ target, path string }{
		{target: "ftp://example.test", path: "/"},
		{target: "http:///missing", path: "/"},
		{target: "http://user@example.test", path: "/"},
		{target: "http://example.test/#fragment", path: "/"},
		{target: "http://example.test/?query", path: "/"},
		{target: "http://example.test/?", path: "/"},
		{target: "http://example.test", path: "relative"},
		{target: "http://example.test", path: "/%zz"},
	}
	for _, test := range tests {
		if got, err := composeURL(test.target, test.path, ""); err == nil {
			t.Fatalf("composeURL(%q, %q) = %v, want error", test.target, test.path, got)
		}
	}
}

func TestComposeURLPreservesAuthorityQueryAndEscapedSuffixProperty(t *testing.T) {
	property := func(segment, query string) bool {
		segment = url.PathEscape(segment)
		query = strings.ReplaceAll(query, "#", "%23")
		got, err := composeURL("https://example.test/prefix", "/"+segment, query)
		return err == nil && got.Host == "example.test" && got.RawQuery == query && strings.HasSuffix(got.EscapedPath(), "/"+segment)
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func FuzzComposeURL(f *testing.F) {
	f.Add("https://example.test/base", "/a%2Fb", "x=1&x=2")
	f.Add("http://localhost:8080", "/", "")
	f.Fuzz(func(t *testing.T, target, path, query string) {
		got, err := composeURL(target, path, query)
		if err != nil {
			return
		}
		if got.Scheme != "http" && got.Scheme != "https" || got.Host == "" || got.User != nil || got.Fragment != "" || got.RawQuery != query {
			t.Fatalf("unsafe composed URL: %#v", got)
		}
	})
}
