package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"time"

	"github.com/logocomune/requestinspector-relay/internal/capture"
)

var errResponseBodyTooLarge = errors.New("upstream response body exceeds configured limit")

type Options struct {
	Transport http.RoundTripper
	Now       func() time.Time
}

type Handler struct {
	transport  http.RoundTripper
	now        func() time.Time
	mutex      sync.Mutex
	transports map[int64]*http.Transport
}

func NewHandler(options Options) *Handler {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Handler{transport: options.Transport, now: now, transports: make(map[int64]*http.Transport)}
}

func (handler *Handler) Handle(parent context.Context, incoming capture.Request, snapshot capture.ConfigSnapshot) (capture.ModeResult, error) {
	destination, err := composeURL(snapshot.Upstream.URL, incoming.Path, incoming.RawQuery)
	if err != nil {
		return capture.ModeResult{}, failure(http.StatusBadGateway, "invalid_upstream", "Configured upstream URL is invalid.", err)
	}
	result := capture.ModeResult{EffectiveUpstream: destination.String()}
	requestInspection := inspect(incoming.Body, incoming.Headers.Get("Content-Encoding"), snapshot.Preview, snapshot.Limits.RequestBodyBytes)
	result.RequestInspection = &requestInspection
	if incoming.Method == http.MethodConnect || hasUpgrade(incoming.Headers) {
		return result, failure(http.StatusNotImplemented, "unsupported_upgrade", "HTTP upgrades and CONNECT tunnels are not supported.", nil)
	}

	timeoutContext, cancel := context.WithTimeout(parent, snapshot.Upstream.Timeout)
	defer cancel()
	recorder := newTraceRecorder(handler.now)
	traceContext := httptrace.WithClientTrace(timeoutContext, recorder.trace())
	request, err := buildRequest(traceContext, destination.String(), incoming)
	if err != nil {
		return result, failure(http.StatusBadGateway, "upstream_request_failed", "Upstream request could not be created.", err)
	}
	started := handler.now()
	recorder.started(started)
	response, err := handler.client(snapshot.Limits.ResponseHeaderBytes).Do(request)
	if err != nil {
		result.ProxyTiming = recorder.timing(nil)
		return result, classifyTransportError(parent, timeoutContext, err)
	}
	if response.StatusCode == http.StatusSwitchingProtocols {
		closeError := response.Body.Close()
		result.ProxyTiming = recorder.timing(nil)
		return result, failure(http.StatusBadGateway, "unsupported_upstream_upgrade", "Upstream attempted an unsupported protocol upgrade.", closeError)
	}

	headers := response.Header.Clone()
	removeHopByHop(headers)
	headerBytes := capture.HeaderBytesEstimate(headers, "")
	result.UpstreamResponseHeaderBytesEstimate = headerBytes
	if headerBytes > int64(snapshot.Limits.ResponseHeaderBytes) {
		closeError := response.Body.Close()
		result.ProxyTiming = recorder.timing(nil)
		return result, failure(http.StatusBadGateway, "response_headers_too_large", "Upstream response headers exceed configured limit.", closeError)
	}
	body, observed, readError := readResponseBody(response.Body, snapshot.Limits.ResponseBodyBytes)
	result.UpstreamObservedResponseBodyBytes = observed
	closeError := response.Body.Close()
	if readError == nil && closeError != nil {
		readError = fmt.Errorf("close upstream response: %w", closeError)
	}
	if readError != nil {
		result.ProxyTiming = recorder.timing(nil)
		if errors.Is(readError, errResponseBodyTooLarge) {
			return result, failure(http.StatusBadGateway, "response_body_too_large", "Upstream response body exceeds configured limit.", readError)
		}
		if errors.Is(timeoutContext.Err(), context.DeadlineExceeded) {
			return result, failure(http.StatusGatewayTimeout, "upstream_timeout", "Upstream request timed out.", readError)
		}
		if errors.Is(parent.Err(), context.Canceled) {
			return result, parent.Err()
		}
		return result, failure(http.StatusBadGateway, "response_body_read_failed", "Upstream response body could not be read.", readError)
	}
	capturedAt := handler.now()
	result.ProxyTiming = recorder.timing(&capturedAt)
	responseInspection := inspect(body, headers.Get("Content-Encoding"), snapshot.Preview, snapshot.Limits.ResponseBodyBytes)
	adjustContentLength(headers, incoming.Method, response.StatusCode, len(body))
	result.CapturedResponse = &capture.Response{
		Status: response.StatusCode, Headers: headers.Clone(), Body: append([]byte(nil), body...),
		ObservedBodyBytes: observed, BodyComplete: true, HeaderBytesEstimate: capture.HeaderBytesEstimate(headers, ""),
		Origin: "upstream", Preview: responseInspection.Preview, PreviewTruncated: responseInspection.PreviewTruncated,
		DecodedPreviewBytes: responseInspection.DecodedPreviewBytes, PreviewError: responseInspection.Error,
	}
	result.Response = capture.DownstreamResponse{Status: response.StatusCode, Headers: headers, Body: body}
	return result, nil
}

func buildRequest(ctx context.Context, destination string, incoming capture.Request) (*http.Request, error) {
	body := io.NopCloser(bytes.NewReader(incoming.Body))
	request, err := http.NewRequestWithContext(ctx, incoming.Method, destination, body)
	if err != nil {
		return nil, err
	}
	request.GetBody = nil
	request.ContentLength = int64(len(incoming.Body))
	request.Host = request.URL.Host
	request.Header = incoming.Headers.Clone()
	removeHopByHop(request.Header)
	request.Header.Del("Content-Length")
	setForwardingHeaders(request.Header, incoming.RemoteAddress, incoming.Host, incoming.Scheme)
	return request, nil
}

func (handler *Handler) client(headerLimit int) *http.Client {
	return &http.Client{
		Transport:     handler.roundTripper(int64(headerLimit)),
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (handler *Handler) roundTripper(headerLimit int64) http.RoundTripper {
	if handler.transport != nil {
		return handler.transport
	}
	handler.mutex.Lock()
	defer handler.mutex.Unlock()
	if transport := handler.transports[headerLimit]; transport != nil {
		return transport
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableCompression = true
	transport.MaxResponseHeaderBytes = headerLimit
	handler.transports[headerLimit] = transport
	return transport
}

func classifyTransportError(parent, timeoutContext context.Context, err error) error {
	if errors.Is(parent.Err(), context.Canceled) {
		return parent.Err()
	}
	if errors.Is(timeoutContext.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return failure(http.StatusGatewayTimeout, "upstream_timeout", "Upstream request timed out.", err)
	}
	if strings.Contains(err.Error(), "response headers exceeded") {
		return failure(http.StatusBadGateway, "response_headers_too_large", "Upstream response headers exceed configured limit.", err)
	}
	return failure(http.StatusBadGateway, "upstream_unavailable", "Upstream connection could not be established.", err)
}

func failure(status int, category, message string, cause error) error {
	return &capture.ModeError{Status: status, Category: category, Message: message, Cause: cause}
}

func readResponseBody(reader io.Reader, limit int64) ([]byte, int64, error) {
	if limit <= 0 {
		return nil, 0, errors.New("response body limit must be positive")
	}
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, int64(len(data)), fmt.Errorf("read upstream response: %w", err)
	}
	observed := int64(len(data))
	if observed > limit {
		return data[:limit], observed, errResponseBodyTooLarge
	}
	return data, observed, nil
}

func adjustContentLength(headers http.Header, method string, status, bodyLength int) {
	if method == http.MethodHead || status == http.StatusNotModified {
		return
	}
	if status >= 100 && status < 200 || status == http.StatusNoContent || status == http.StatusResetContent {
		headers.Del("Content-Length")
		return
	}
	headers.Set("Content-Length", fmt.Sprintf("%d", bodyLength))
}

type traceRecorder struct {
	now               func() time.Time
	mutex             sync.Mutex
	upstreamStarted   time.Time
	requestWritten    time.Time
	firstResponseByte time.Time
}

func newTraceRecorder(now func() time.Time) *traceRecorder { return &traceRecorder{now: now} }

func (recorder *traceRecorder) started(at time.Time) {
	recorder.mutex.Lock()
	recorder.upstreamStarted = at
	recorder.mutex.Unlock()
}

func (recorder *traceRecorder) trace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		WroteRequest: func(httptrace.WroteRequestInfo) {
			recorder.mutex.Lock()
			recorder.requestWritten = recorder.now()
			recorder.mutex.Unlock()
		},
		GotFirstResponseByte: func() {
			recorder.mutex.Lock()
			recorder.firstResponseByte = recorder.now()
			recorder.mutex.Unlock()
		},
	}
}

func (recorder *traceRecorder) timing(capturedAt *time.Time) *capture.ProxyTiming {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	timing := &capture.ProxyTiming{}
	setTime := func(value time.Time) *time.Time {
		if value.IsZero() {
			return nil
		}
		utc := value.UTC()
		return &utc
	}
	duration := func(started, completed time.Time) *int64 {
		if started.IsZero() || completed.IsZero() || completed.Before(started) {
			return nil
		}
		value := completed.Sub(started).Microseconds()
		return &value
	}
	timing.UpstreamStartedAt = setTime(recorder.upstreamStarted)
	timing.RequestWrittenAt = setTime(recorder.requestWritten)
	timing.FirstResponseByteAt = setTime(recorder.firstResponseByte)
	if capturedAt != nil {
		timing.ResponseCapturedAt = setTime(*capturedAt)
	}
	timing.UpstreamDispatchDurationUS = duration(recorder.upstreamStarted, recorder.requestWritten)
	timing.UpstreamWaitDurationUS = duration(recorder.requestWritten, recorder.firstResponseByte)
	if capturedAt != nil {
		timing.UpstreamReceiveDurationUS = duration(recorder.firstResponseByte, *capturedAt)
		if timing.UpstreamDispatchDurationUS != nil && timing.UpstreamWaitDurationUS != nil && timing.UpstreamReceiveDurationUS != nil {
			total := *timing.UpstreamDispatchDurationUS + *timing.UpstreamWaitDurationUS + *timing.UpstreamReceiveDurationUS
			timing.UpstreamTotalDurationUS = &total
		} else {
			timing.UpstreamTotalDurationUS = duration(recorder.upstreamStarted, *capturedAt)
		}
	}
	return timing
}
