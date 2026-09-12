package capture_test

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"testing/quick"

	"github.com/logocomune/requestinspector-relay/internal/capture"
	"github.com/logocomune/requestinspector-relay/internal/config"
)

func TestCaptureHandlerBuildsExactConfiguredResponse(t *testing.T) {
	cfg := config.Defaults()
	cfg.Capture.Status = http.StatusCreated
	cfg.Capture.Headers = map[string][]string{
		"Content-Type": {"application/octet-stream"},
		"X-Repeated":   {"one", "two"},
	}
	cfg.Capture.Body = "\x00reply\xff"
	snapshot := capture.NewConfigSnapshot(cfg, 7)

	result, err := (capture.CaptureHandler{}).Handle(context.Background(), capture.Request{Method: http.MethodPost}, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if result.Response.Status != http.StatusCreated || !reflect.DeepEqual(result.Response.Headers, http.Header(cfg.Capture.Headers)) || string(result.Response.Body) != cfg.Capture.Body {
		t.Fatalf("response = %+v", result.Response)
	}
	if result.CapturedResponse != nil || result.EffectiveUpstream != "" {
		t.Fatalf("capture metadata leaked generated response: %+v", result)
	}

	result.Response.Headers.Set("X-Repeated", "changed")
	if snapshot.Capture.Headers["X-Repeated"][0] != "one" {
		t.Fatalf("response headers alias snapshot: %+v", snapshot.Capture.Headers)
	}
}

func TestCaptureHandlerSuppressesForbiddenResponseBodies(t *testing.T) {
	tests := []struct {
		name   string
		method string
		status int
	}{
		{name: "head", method: http.MethodHead, status: http.StatusOK},
		{name: "no content", method: http.MethodGet, status: http.StatusNoContent},
		{name: "reset content", method: http.MethodGet, status: http.StatusResetContent},
		{name: "not modified", method: http.MethodGet, status: http.StatusNotModified},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.Capture.Status = test.status
			cfg.Capture.Body = "must-not-be-written"
			result, err := (capture.CaptureHandler{}).Handle(context.Background(), capture.Request{Method: test.method}, capture.NewConfigSnapshot(cfg, 1))
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Response.Body) != 0 {
				t.Fatalf("body = %q", result.Response.Body)
			}
		})
	}
}

func TestCaptureHandlerAllowsBodyForOrdinaryStatuses(t *testing.T) {
	property := func(statusOffset uint8, body string) bool {
		cfg := config.Defaults()
		cfg.Capture.Status = http.StatusOK + int(statusOffset)%400
		if cfg.Capture.Status == http.StatusNoContent || cfg.Capture.Status == http.StatusResetContent || cfg.Capture.Status == http.StatusNotModified {
			return true
		}
		cfg.Capture.Body = body
		result, err := (capture.CaptureHandler{}).Handle(context.Background(), capture.Request{Method: http.MethodGet}, capture.NewConfigSnapshot(cfg, 1))
		return err == nil && string(result.Response.Body) == body
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func FuzzCaptureHandler(f *testing.F) {
	f.Add(http.MethodPost, uint16(http.StatusCreated), "reply\x00\xff")
	f.Add(http.MethodHead, uint16(http.StatusOK), "hidden")
	f.Add(http.MethodGet, uint16(http.StatusNoContent), "hidden")
	f.Fuzz(func(t *testing.T, method string, statusValue uint16, body string) {
		status := http.StatusOK + int(statusValue)%400
		cfg := config.Defaults()
		cfg.Capture.Status = status
		cfg.Capture.Body = body
		result, err := (capture.CaptureHandler{}).Handle(context.Background(), capture.Request{Method: method}, capture.NewConfigSnapshot(cfg, 1))
		if err != nil {
			t.Fatal(err)
		}
		forbidden := method == http.MethodHead || status == http.StatusNoContent || status == http.StatusResetContent || status == http.StatusNotModified
		if forbidden && len(result.Response.Body) != 0 {
			t.Fatalf("forbidden response body = %q", result.Response.Body)
		}
		if !forbidden && string(result.Response.Body) != body {
			t.Fatalf("response body = %q, want %q", result.Response.Body, body)
		}
	})
}
