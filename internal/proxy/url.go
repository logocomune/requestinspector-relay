package proxy

import (
	"errors"
	"net/url"
	"strings"
)

func composeURL(target, incomingEscapedPath, rawQuery string) (*url.URL, error) {
	destination, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(destination.Scheme, "http") && !strings.EqualFold(destination.Scheme, "https") {
		return nil, errors.New("upstream scheme must be http or https")
	}
	if destination.Host == "" {
		return nil, errors.New("upstream authority is required")
	}
	if destination.User != nil || destination.Fragment != "" || destination.RawQuery != "" || destination.ForceQuery {
		return nil, errors.New("upstream must not contain userinfo, fragment, or query")
	}
	destination.Scheme = strings.ToLower(destination.Scheme)
	if incomingEscapedPath == "" {
		incomingEscapedPath = "/"
	}
	if !strings.HasPrefix(incomingEscapedPath, "/") {
		return nil, errors.New("incoming path must be absolute")
	}
	escapedPath := joinEscapedPath(destination.EscapedPath(), incomingEscapedPath)
	path, err := url.PathUnescape(escapedPath)
	if err != nil {
		return nil, errors.New("incoming path has invalid escaping")
	}
	destination.Path = path
	destination.RawPath = escapedPath
	destination.RawQuery = rawQuery
	destination.ForceQuery = false
	return destination, nil
}

func joinEscapedPath(prefix, path string) string {
	if prefix == "" || prefix == "/" {
		return path
	}
	if strings.HasSuffix(prefix, "/") {
		return prefix + strings.TrimPrefix(path, "/")
	}
	return prefix + "/" + strings.TrimPrefix(path, "/")
}
