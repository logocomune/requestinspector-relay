package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/logocomune/requestinspector-relay/internal/config"
)

func TestAuthenticatorDisabledExpiryCapacityAndPruning(t *testing.T) {
	now := time.Unix(100, 0)
	disabled := newAuthenticator(config.Authentication{SessionTTL: time.Hour}, func() time.Time { return now })
	if token, _, code := disabled.login("peer", "", ""); token != "" || code != "authentication_disabled" {
		t.Fatalf("disabled login token=%q code=%q", token, code)
	}

	auth := newAuthenticator(config.Authentication{Username: "user", Password: "pass", SessionTTL: time.Second}, func() time.Time { return now })
	auth.maxSessions = 1
	first, _, code := auth.login("127.0.0.1:1", "user", "pass")
	if code != "" {
		t.Fatal(code)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: first})
	now = now.Add(2 * time.Second)
	second, _, code := auth.login("127.0.0.2:2", "user", "pass")
	if code != "" || second == "" || auth.valid(request) {
		t.Fatalf("expired replacement token=%q code=%q old-valid=%v", second, code, auth.valid(request))
	}

	auth.attempts = map[string][]time.Time{"expired": {now.Add(-2 * time.Minute)}}
	auth.maxAttempts = 1
	if _, _, code := auth.login("new:1", "user", "wrong"); code != "invalid_credentials" {
		t.Fatalf("pruned login code=%q attempts=%v", code, auth.attempts)
	}
	auth.attempts = map[string][]time.Time{"active": {now}}
	if _, _, code := auth.login("blocked:1", "user", "pass"); code != "login_rate_limited" {
		t.Fatalf("bounded attempt map code=%q", code)
	}
	auth.logout(httptest.NewRequest(http.MethodDelete, "/", nil))
}

func TestMutationOriginValidation(t *testing.T) {
	tests := []struct {
		origin string
		host   string
		want   bool
	}{
		{"", "example.test", true},
		{"http://example.test", "example.test", true},
		{"https://example.test", "example.test", false},
		{"://bad", "example.test", false},
		{"http://other.test", "example.test", false},
	}
	for _, test := range tests {
		request := httptest.NewRequest(http.MethodPost, "http://"+test.host+"/api", nil)
		request.Header.Set("Origin", test.origin)
		if got := validMutationOrigin(request); got != test.want {
			t.Errorf("origin %q host %q = %v", test.origin, test.host, got)
		}
	}
}
