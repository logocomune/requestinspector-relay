package proxy

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"testing/quick"
	"time"

	"github.com/logocomune/requestinspector-relay/internal/capture"
	"github.com/logocomune/requestinspector-relay/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestHandlerForwardsAndCapturesExactRepresentations(t *testing.T) {
	requestBody := gzipBytes(t, []byte(`{"request":true}`))
	responseBody := gzipBytes(t, []byte(`{"response":true}`))
	var upstreamHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
		}
		if request.Method != http.MethodPatch || request.URL.EscapedPath() != "/base%2Fv1/orders%2Fspecial" || request.URL.RawQuery != "b=2&a=1&a=3" {
			t.Errorf("request target = %s %s", request.Method, request.URL.String())
		}
		if request.Host != upstreamHost || !bytes.Equal(body, requestBody) || request.ContentLength != int64(len(requestBody)) {
			t.Errorf("host=%q want-host=%q length=%d body-identical=%v", request.Host, upstreamHost, request.ContentLength, bytes.Equal(body, requestBody))
		}
		if values := request.Header.Values("X-Multi"); len(values) != 2 || values[0] != "one" || values[1] != "two" {
			t.Errorf("X-Multi = %v", values)
		}
		if request.Header.Get("X-Remove") != "" || request.Header.Get("Connection") != "" {
			t.Errorf("hop headers leaked: %v", request.Header)
		}
		if request.Header.Get("X-Forwarded-For") != "192.0.2.10" || request.Header.Get("X-Forwarded-Host") != "incoming.example" || request.Header.Get("X-Forwarded-Proto") != "https" {
			t.Errorf("forwarding headers = %v", request.Header)
		}
		writer.Header().Add("Set-Cookie", "a=1")
		writer.Header().Add("Set-Cookie", "b=2")
		writer.Header().Set("Content-Encoding", "gzip")
		writer.Header().Set("Connection", "X-Upstream-Hop")
		writer.Header().Set("X-Upstream-Hop", "remove")
		writer.WriteHeader(http.StatusTeapot)
		if _, err := writer.Write(responseBody); err != nil {
			t.Error(err)
		}
	}))
	defer upstream.Close()
	upstreamHost = upstream.Listener.Addr().String()

	snapshot := proxySnapshot(upstream.URL + "/base%2Fv1")
	incoming := capture.Request{
		Method: http.MethodPatch, Scheme: "https", Host: "incoming.example", Path: "/orders%2Fspecial", RawQuery: "b=2&a=1&a=3",
		RemoteAddress: "192.0.2.10:4321", Body: requestBody,
		Headers: http.Header{
			"Content-Encoding": {"gzip"}, "X-Multi": {"one", "two"}, "Connection": {"X-Remove"},
			"X-Remove": {"secret"}, "X-Forwarded-For": {"spoofed"}, "Forwarded": {"for=spoofed"},
		},
	}
	result, err := NewHandler(Options{}).Handle(context.Background(), incoming, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if result.Response.Status != http.StatusTeapot || !bytes.Equal(result.Response.Body, responseBody) || result.CapturedResponse == nil || !bytes.Equal(result.CapturedResponse.Body, responseBody) {
		t.Fatalf("result = %+v", result)
	}
	if result.CapturedResponse.Origin != "upstream" || result.CapturedResponse.Headers.Get("X-Upstream-Hop") != "" || len(result.CapturedResponse.Headers.Values("Set-Cookie")) != 2 {
		t.Fatalf("captured response = %+v", result.CapturedResponse)
	}
	if result.RequestInspection == nil || string(result.RequestInspection.Preview) != `{"request":true}` || string(result.CapturedResponse.Preview) != `{"response":true}` {
		t.Fatalf("previews request=%+v response=%+v", result.RequestInspection, result.CapturedResponse)
	}
	assertCompleteTiming(t, result.ProxyTiming)
}

func TestHandlerReturnsRedirectWithoutFollowing(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.URL.Path == "/next" {
			t.Fatal("redirect followed")
		}
		http.Redirect(writer, request, "/next", http.StatusFound)
	}))
	defer upstream.Close()
	result, err := NewHandler(Options{}).Handle(context.Background(), testRequest(), proxySnapshot(upstream.URL))
	if err != nil || result.Response.Status != http.StatusFound || requests.Load() != 1 {
		t.Fatalf("status=%d requests=%d error=%v", result.Response.Status, requests.Load(), err)
	}
}

func TestHandlerPreservesUpstreamApplicationErrors(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(status)
				if _, err := writer.Write([]byte(http.StatusText(status))); err != nil {
					t.Error(err)
				}
			}))
			defer upstream.Close()
			result, err := NewHandler(Options{}).Handle(context.Background(), testRequest(), proxySnapshot(upstream.URL))
			if err != nil || result.Response.Status != status || string(result.Response.Body) != http.StatusText(status) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}

func TestHandlerClassifiesTLSAndConnectFailures(t *testing.T) {
	tlsUpstream := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	_, tlsError := NewHandler(Options{}).Handle(context.Background(), testRequest(), proxySnapshot(tlsUpstream.URL))
	tlsUpstream.Close()
	assertModeError(t, tlsError, http.StatusBadGateway, "upstream_unavailable")

	closedUpstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedURL := closedUpstream.URL
	closedUpstream.Close()
	_, connectError := NewHandler(Options{}).Handle(context.Background(), testRequest(), proxySnapshot(closedURL))
	assertModeError(t, connectError, http.StatusBadGateway, "upstream_unavailable")
}

func TestHandlerDoesNotRetryTransportErrors(t *testing.T) {
	var calls atomic.Int32
	handler := NewHandler(Options{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, io.ErrUnexpectedEOF
	})})
	_, err := handler.Handle(context.Background(), testRequest(), proxySnapshot("http://example.test"))
	assertModeError(t, err, http.StatusBadGateway, "upstream_unavailable")
	if calls.Load() != 1 {
		t.Fatalf("transport calls = %d", calls.Load())
	}
}

func TestHandlerRejectsUnsupportedUpgradeBeforeTransport(t *testing.T) {
	var calls atomic.Int32
	handler := NewHandler(Options{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected call")
	})})
	tests := []capture.Request{
		{Method: http.MethodConnect, Path: "/", Headers: make(http.Header)},
		{Method: http.MethodGet, Path: "/", Headers: http.Header{"Connection": {"keep-alive, Upgrade"}, "Upgrade": {"websocket"}}},
	}
	for _, request := range tests {
		_, err := handler.Handle(context.Background(), request, proxySnapshot("http://example.test"))
		assertModeError(t, err, http.StatusNotImplemented, "unsupported_upgrade")
	}
	if calls.Load() != 0 {
		t.Fatalf("transport calls = %d", calls.Load())
	}
}

func TestHandlerResponseLimits(t *testing.T) {
	tests := []struct {
		name     string
		response *http.Response
		limit    func(*capture.ConfigSnapshot)
		category string
	}{
		{
			name: "body", response: response(http.StatusOK, http.Header{}, []byte("12345")),
			limit: func(snapshot *capture.ConfigSnapshot) { snapshot.Limits.ResponseBodyBytes = 4 }, category: "response_body_too_large",
		},
		{
			name: "headers", response: response(http.StatusOK, http.Header{"X-Large": {"12345"}}, nil),
			limit: func(snapshot *capture.ConfigSnapshot) { snapshot.Limits.ResponseHeaderBytes = 4 }, category: "response_headers_too_large",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := NewHandler(Options{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return test.response, nil })})
			snapshot := proxySnapshot("http://example.test")
			test.limit(&snapshot)
			result, err := handler.Handle(context.Background(), testRequest(), snapshot)
			assertModeError(t, err, http.StatusBadGateway, test.category)
			if result.CapturedResponse != nil {
				t.Fatalf("partial upstream response exposed: %+v", result.CapturedResponse)
			}
		})
	}
}

func TestHandlerRejectsUpstreamUpgrade(t *testing.T) {
	handler := NewHandler(Options{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusSwitchingProtocols, http.Header{"Upgrade": {"websocket"}}, nil), nil
	})})
	_, err := handler.Handle(context.Background(), testRequest(), proxySnapshot("http://example.test"))
	assertModeError(t, err, http.StatusBadGateway, "unsupported_upstream_upgrade")
}

func TestHandlerClassifiesResponseReadAndCloseFailures(t *testing.T) {
	tests := []io.ReadCloser{
		structReadCloser{Reader: errorReader{}},
		closeErrorBody{Reader: bytes.NewReader(nil)},
	}
	for _, body := range tests {
		handler := NewHandler(Options{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}, nil
		})})
		result, err := handler.Handle(context.Background(), testRequest(), proxySnapshot("http://example.test"))
		assertModeError(t, err, http.StatusBadGateway, "response_body_read_failed")
		if result.UpstreamResponseHeaderBytesEstimate == 0 {
			t.Fatalf("missing observed header size: %+v", result)
		}
	}
}

func TestHandlerTimeoutAndCancellation(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	t.Run("timeout", func(t *testing.T) {
		snapshot := proxySnapshot("http://example.test")
		snapshot.Upstream.Timeout = time.Millisecond
		_, err := NewHandler(Options{Transport: transport}).Handle(context.Background(), testRequest(), snapshot)
		assertModeError(t, err, http.StatusGatewayTimeout, "upstream_timeout")
	})
	t.Run("cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := NewHandler(Options{Transport: transport}).Handle(ctx, testRequest(), proxySnapshot("http://example.test"))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestHandlerRejectsInvalidRuntimeTarget(t *testing.T) {
	_, err := NewHandler(Options{}).Handle(context.Background(), testRequest(), proxySnapshot("http://user@example.test"))
	assertModeError(t, err, http.StatusBadGateway, "invalid_upstream")
}

func TestReadResponseBodyFailures(t *testing.T) {
	if _, _, err := readResponseBody(bytes.NewReader(nil), 0); err == nil {
		t.Fatal("zero limit accepted")
	}
	body, observed, err := readResponseBody(errorReader{}, 10)
	if body != nil || observed != 0 || err == nil {
		t.Fatalf("readResponseBody() = %v, %d, %v", body, observed, err)
	}
}

func TestBuildRequestPreservesBodyProperty(t *testing.T) {
	property := func(body []byte) bool {
		incoming := testRequest()
		incoming.Method = http.MethodPost
		incoming.Body = body
		request, err := buildRequest(context.Background(), "http://example.test/path", incoming)
		if err != nil {
			return false
		}
		forwarded, err := io.ReadAll(request.Body)
		return err == nil && bytes.Equal(forwarded, body) && request.ContentLength == int64(len(body)) && request.GetBody == nil
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func TestBuildRequestRejectsInvalidMethod(t *testing.T) {
	incoming := testRequest()
	incoming.Method = "GET\n"
	if _, err := buildRequest(context.Background(), "http://example.test", incoming); err == nil {
		t.Fatal("invalid method accepted")
	}
}

func TestHandlerCachesDefaultTransportByHeaderLimit(t *testing.T) {
	handler := NewHandler(Options{})
	first := handler.roundTripper(1024)
	if first != handler.roundTripper(1024) || first == handler.roundTripper(2048) {
		t.Fatal("transport cache does not follow header limit")
	}
}

func TestClassifyTransportHeaderLimit(t *testing.T) {
	err := classifyTransportError(context.Background(), context.Background(), errors.New("net/http: server response headers exceeded 1024 bytes; aborted"))
	assertModeError(t, err, http.StatusBadGateway, "response_headers_too_large")
}

func TestAdjustContentLength(t *testing.T) {
	tests := []struct {
		name, method string
		status       int
		want         string
	}{
		{name: "body", method: http.MethodGet, status: http.StatusOK, want: "3"},
		{name: "head", method: http.MethodHead, status: http.StatusOK, want: "9"},
		{name: "not modified", method: http.MethodGet, status: http.StatusNotModified, want: "9"},
		{name: "no content", method: http.MethodGet, status: http.StatusNoContent},
		{name: "informational", method: http.MethodGet, status: http.StatusContinue},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			headers := http.Header{"Content-Length": {"9"}}
			adjustContentLength(headers, test.method, test.status, 3)
			if got := headers.Get("Content-Length"); got != test.want {
				t.Fatalf("Content-Length = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPeerHost(t *testing.T) {
	tests := map[string]string{"192.0.2.1:80": "192.0.2.1", "[2001:db8::1]:443": "2001:db8::1", "192.0.2.2": "192.0.2.2", "invalid": ""}
	for input, want := range tests {
		if got := peerHost(input); got != want {
			t.Fatalf("peerHost(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestTraceTimingAdditivityProperty(t *testing.T) {
	property := func(dispatch, wait, receive uint32) bool {
		start := time.Unix(1, 0)
		written := start.Add(time.Duration(dispatch) * time.Nanosecond)
		first := written.Add(time.Duration(wait) * time.Nanosecond)
		captured := first.Add(time.Duration(receive) * time.Nanosecond)
		recorder := &traceRecorder{upstreamStarted: start, requestWritten: written, firstResponseByte: first}
		timing := recorder.timing(&captured)
		return *timing.UpstreamTotalDurationUS == *timing.UpstreamDispatchDurationUS+*timing.UpstreamWaitDurationUS+*timing.UpstreamReceiveDurationUS
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func response(status int, headers http.Header, body []byte) *http.Response {
	return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(bytes.NewReader(body))}
}

func proxySnapshot(upstream string) capture.ConfigSnapshot {
	cfg := config.Defaults()
	cfg.Mode = config.ModeProxy
	cfg.Upstream.URL = upstream
	cfg.Preview.Bytes = 1024
	return capture.NewConfigSnapshot(cfg, 1)
}

func testRequest() capture.Request {
	return capture.Request{Method: http.MethodGet, Scheme: "http", Host: "incoming.test", Path: "/", RemoteAddress: "192.0.2.1:1234", Headers: make(http.Header)}
}

func assertModeError(t *testing.T, err error, status int, category string) {
	t.Helper()
	var failure *capture.ModeError
	if !errors.As(err, &failure) || failure.Status != status || failure.Category != category {
		t.Fatalf("error = %#v, want status=%d category=%q", err, status, category)
	}
}

func assertCompleteTiming(t *testing.T, timing *capture.ProxyTiming) {
	t.Helper()
	if timing == nil || timing.UpstreamStartedAt == nil || timing.RequestWrittenAt == nil || timing.FirstResponseByteAt == nil || timing.ResponseCapturedAt == nil || timing.UpstreamDispatchDurationUS == nil || timing.UpstreamWaitDurationUS == nil || timing.UpstreamReceiveDurationUS == nil || timing.UpstreamTotalDurationUS == nil {
		t.Fatalf("incomplete timing: %+v", timing)
	}
	for _, duration := range []*int64{timing.UpstreamDispatchDurationUS, timing.UpstreamWaitDurationUS, timing.UpstreamReceiveDurationUS, timing.UpstreamTotalDurationUS} {
		if *duration < 0 {
			t.Fatalf("negative duration: %+v", timing)
		}
	}
}

func gzipBytes(t *testing.T, body []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

type structReadCloser struct{ io.Reader }

func (structReadCloser) Close() error { return nil }

type closeErrorBody struct{ io.Reader }

func (closeErrorBody) Close() error { return io.ErrClosedPipe }

func FuzzReadResponseBody(f *testing.F) {
	f.Add([]byte("response"), uint16(8))
	f.Add([]byte{0, 255, 1}, uint16(2))
	f.Fuzz(func(t *testing.T, data []byte, rawLimit uint16) {
		limit := int64(rawLimit) + 1
		body, observed, err := readResponseBody(bytes.NewReader(data), limit)
		if int64(len(data)) <= limit {
			if err != nil || !bytes.Equal(body, data) || observed != int64(len(data)) {
				t.Fatalf("accepted body mismatch")
			}
			return
		}
		if !errors.Is(err, errResponseBodyTooLarge) || int64(len(body)) != limit || observed != limit+1 {
			t.Fatalf("oversized result len=%d observed=%d error=%v", len(body), observed, err)
		}
	})
}
