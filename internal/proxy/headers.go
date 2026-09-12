package proxy

import (
	"net"
	"net/http"
	"strings"
)

var hopByHopHeaders = []string{
	"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
	"Proxy-Authorization", "TE", "Trailer", "Transfer-Encoding", "Upgrade",
}

func removeHopByHop(headers http.Header) {
	for _, value := range headers.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			headers.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range hopByHopHeaders {
		headers.Del(name)
	}
}

func hasUpgrade(headers http.Header) bool {
	if headers.Get("Upgrade") != "" {
		return true
	}
	for _, value := range headers.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), "upgrade") {
				return true
			}
		}
	}
	return false
}

func setForwardingHeaders(headers http.Header, remoteAddress, originalHost, scheme string) {
	for _, name := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto"} {
		headers.Del(name)
	}
	if host := peerHost(remoteAddress); host != "" {
		headers.Set("X-Forwarded-For", host)
	}
	if originalHost != "" {
		headers.Set("X-Forwarded-Host", originalHost)
	}
	if scheme == "http" || scheme == "https" {
		headers.Set("X-Forwarded-Proto", scheme)
	}
}

func peerHost(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err == nil {
		return host
	}
	if net.ParseIP(address) != nil {
		return address
	}
	return ""
}
