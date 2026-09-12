package cors

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerDisabledPassesThrough(t *testing.T) {
	called := false
	handler := Handler(func() bool { return false }, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		called = true
		writer.WriteHeader(http.StatusCreated)
	}))

	request := httptest.NewRequest(http.MethodPost, "/ingest", nil)
	request.Header.Set("Origin", "https://client.example")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if !called || recorder.Code != http.StatusCreated {
		t.Fatalf("disabled CORS pass-through = called %v status %d", called, recorder.Code)
	}
	if value := recorder.Header().Get("Access-Control-Allow-Origin"); value != "" {
		t.Fatalf("disabled CORS header = %q, want empty", value)
	}
}

func TestHandlerAllowsConfiguredCrossOriginRequest(t *testing.T) {
	handler := Handler(func() bool { return true }, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusAccepted)
	}))

	request := httptest.NewRequest(http.MethodPost, "/ingest", nil)
	request.Header.Set("Origin", "https://client.example")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusAccepted)
	}
	if value := recorder.Header().Get("Access-Control-Allow-Origin"); value != "https://client.example" {
		t.Fatalf("allow origin = %q, want request origin", value)
	}
	if value := recorder.Header().Get("Vary"); value != "Origin" {
		t.Fatalf("vary = %q, want Origin", value)
	}
}

func TestHandlerEnabledWithoutOriginPassesThrough(t *testing.T) {
	called := false
	handler := Handler(func() bool { return true }, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		called = true
		writer.WriteHeader(http.StatusAccepted)
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/ingest", nil))

	if !called || recorder.Code != http.StatusAccepted {
		t.Fatalf("no-origin pass-through = called %v status %d", called, recorder.Code)
	}
	if value := recorder.Header().Get("Access-Control-Allow-Origin"); value != "" {
		t.Fatalf("no-origin allow origin = %q, want empty", value)
	}
}

func TestHandlerAnswersPreflightWithoutCallingIngest(t *testing.T) {
	called := false
	handler := Handler(func() bool { return true }, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		called = true
	}))

	request := httptest.NewRequest(http.MethodOptions, "/ingest", nil)
	request.Header.Set("Origin", "https://client.example")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "Content-Type, X-Test")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if called {
		t.Fatal("preflight called ingest handler")
	}
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if value := recorder.Header().Get("Access-Control-Allow-Origin"); value != "https://client.example" {
		t.Fatalf("preflight allow origin = %q, want request origin", value)
	}
	if value := recorder.Header().Get("Vary"); value != "Origin, Access-Control-Request-Method, Access-Control-Request-Headers" {
		t.Fatalf("preflight vary = %q", value)
	}
	if value := recorder.Header().Get("Access-Control-Allow-Methods"); value != "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS" {
		t.Fatalf("preflight allow methods = %q", value)
	}
	if value := recorder.Header().Get("Access-Control-Allow-Headers"); value != "Content-Type, X-Test" {
		t.Fatalf("preflight allow headers = %q", value)
	}
}

func TestHandlerPreflightDefaultsAllowedHeaders(t *testing.T) {
	handler := Handler(func() bool { return true }, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("preflight called ingest handler")
	}))
	request := httptest.NewRequest(http.MethodOptions, "/ingest", nil)
	request.Header.Set("Origin", "https://client.example")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if value := recorder.Header().Get("Access-Control-Allow-Headers"); value != "*" {
		t.Fatalf("default preflight allow headers = %q, want *", value)
	}
}
