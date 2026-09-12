package httpapi

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/logocomune/requestinspector-relay/internal/config"
)

const sessionCookieName = "reqrelay_session"

type authenticator struct {
	mutex       sync.Mutex
	username    string
	password    string
	ttl         time.Duration
	now         func() time.Time
	sessions    map[string]time.Time
	attempts    map[string][]time.Time
	maxSessions int
	maxAttempts int
}

func newAuthenticator(settings config.Authentication, now func() time.Time) *authenticator {
	if now == nil {
		now = time.Now
	}
	return &authenticator{username: settings.Username, password: settings.Password, ttl: settings.SessionTTL, now: now, sessions: make(map[string]time.Time), attempts: make(map[string][]time.Time), maxSessions: 1024, maxAttempts: 1024}
}

func (auth *authenticator) required() bool { return auth.username != "" }

func (auth *authenticator) login(remoteAddress, username, password string) (string, time.Time, string) {
	auth.mutex.Lock()
	defer auth.mutex.Unlock()
	if !auth.required() {
		return "", time.Time{}, "authentication_disabled"
	}
	now := auth.now()
	key := clientAddress(remoteAddress)
	if _, exists := auth.attempts[key]; !exists && len(auth.attempts) >= auth.maxAttempts {
		auth.pruneAttempts(now)
		if len(auth.attempts) >= auth.maxAttempts {
			return "", time.Time{}, "login_rate_limited"
		}
	}
	recent := auth.recentAttempts(key, now)
	if len(recent) >= 5 {
		auth.attempts[key] = recent
		return "", time.Time{}, "login_rate_limited"
	}
	auth.attempts[key] = append(recent, now)
	if subtle.ConstantTimeCompare([]byte(username), []byte(auth.username)) != 1 || subtle.ConstantTimeCompare([]byte(password), []byte(auth.password)) != 1 {
		return "", time.Time{}, "invalid_credentials"
	}
	if len(auth.sessions) >= auth.maxSessions {
		auth.removeExpired(now)
		if len(auth.sessions) >= auth.maxSessions {
			return "", time.Time{}, "session_capacity_exhausted"
		}
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", time.Time{}, "session_creation_failed"
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	expires := now.Add(auth.ttl)
	auth.sessions[token] = expires
	return token, expires, ""
}

func (auth *authenticator) valid(request *http.Request) bool {
	if !auth.required() {
		return true
	}
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	auth.mutex.Lock()
	defer auth.mutex.Unlock()
	expires, ok := auth.sessions[cookie.Value]
	if !ok || !expires.After(auth.now()) {
		delete(auth.sessions, cookie.Value)
		return false
	}
	return true
}

func (auth *authenticator) logout(request *http.Request) {
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil {
		return
	}
	auth.mutex.Lock()
	delete(auth.sessions, cookie.Value)
	auth.mutex.Unlock()
}

func (auth *authenticator) recentAttempts(key string, now time.Time) []time.Time {
	cutoff := now.Add(-time.Minute)
	values := auth.attempts[key]
	kept := values[:0]
	for _, value := range values {
		if value.After(cutoff) {
			kept = append(kept, value)
		}
	}
	return kept
}

func (auth *authenticator) removeExpired(now time.Time) {
	for token, expires := range auth.sessions {
		if !expires.After(now) {
			delete(auth.sessions, token)
		}
	}
}

func (auth *authenticator) pruneAttempts(now time.Time) {
	for key := range auth.attempts {
		recent := auth.recentAttempts(key, now)
		if len(recent) == 0 {
			delete(auth.attempts, key)
			continue
		}
		auth.attempts[key] = recent
	}
}

func clientAddress(remoteAddress string) string {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err == nil {
		return host
	}
	return remoteAddress
}

func validMutationOrigin(request *http.Request) bool {
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}
	return strings.EqualFold(parsed.Scheme, scheme) && strings.EqualFold(parsed.Host, request.Host)
}
