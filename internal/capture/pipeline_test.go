package capture_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/logocomune/requestinspector-relay/internal/capture"
	"github.com/logocomune/requestinspector-relay/internal/config"
	"github.com/logocomune/requestinspector-relay/internal/events"
	"github.com/logocomune/requestinspector-relay/internal/store"
)

type modeHandler struct {
	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
	result  capture.ModeResult
	err     error
}

type mutatingHandler struct{}

type reservationRepository struct {
	*store.Memory
	accept    bool
	reserved  int64
	committed chan capture.Exchange
}

type testReservation struct {
	committed chan capture.Exchange
}

func (repository *reservationRepository) Reserve(bytes int64) (capture.PersistenceReservation, bool) {
	repository.reserved = bytes
	if !repository.accept {
		return nil, false
	}
	return &testReservation{committed: repository.committed}, true
}

func (reservation *testReservation) Commit(exchange capture.Exchange) error {
	reservation.committed <- exchange.Clone()
	return nil
}

func (reservation *testReservation) Cancel() {}

type waitingHandler struct {
	entered chan struct{}
	release chan struct{}
}

func (handler waitingHandler) Handle(ctx context.Context, _ capture.Request, _ capture.ConfigSnapshot) (capture.ModeResult, error) {
	select {
	case handler.entered <- struct{}{}:
	case <-ctx.Done():
		return capture.ModeResult{}, ctx.Err()
	}
	select {
	case <-handler.release:
		return capture.ModeResult{Response: capture.DownstreamResponse{Status: http.StatusNoContent}}, nil
	case <-ctx.Done():
		return capture.ModeResult{}, ctx.Err()
	}
}

func (mutatingHandler) Handle(_ context.Context, request capture.Request, snapshot capture.ConfigSnapshot) (capture.ModeResult, error) {
	request.Body[0] = 'X'
	request.Headers.Set("X-Test", "changed")
	snapshot.Capture.Headers["X-Snapshot"][0] = "changed"
	return capture.ModeResult{Response: capture.DownstreamResponse{Status: http.StatusNoContent}}, nil
}

func (handler *modeHandler) Handle(ctx context.Context, request capture.Request, snapshot capture.ConfigSnapshot) (capture.ModeResult, error) {
	handler.calls.Add(1)
	if handler.entered != nil {
		close(handler.entered)
	}
	if handler.release != nil {
		select {
		case <-ctx.Done():
			return capture.ModeResult{}, ctx.Err()
		case <-handler.release:
		}
	}
	if handler.err != nil {
		return handler.result, handler.err
	}
	if handler.result.Response.Status != 0 {
		return handler.result, nil
	}
	return capture.ModeResult{Response: capture.DownstreamResponse{Status: http.StatusNoContent}}, nil
}

func TestPipelineTruncatesPreviewWithoutChangingBody(t *testing.T) {
	cfg := config.Defaults()
	cfg.Preview.Bytes = 2
	repository := store.NewMemory(10)
	pipeline := newPipeline(cfg, repository, &modeHandler{})
	pipeline.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("body"))))
	exchange := repository.List()[0]
	if string(exchange.Request.Preview) != "bo" || !exchange.Request.PreviewTruncated || string(exchange.Request.Body) != "body" {
		t.Fatalf("request = %+v", exchange.Request)
	}
}

func TestPipelineDisabledPreviewRetainsCompleteBody(t *testing.T) {
	cfg := config.Defaults()
	cfg.Preview.Enabled = false
	cfg.Preview.Bytes = 1
	repository := store.NewMemory(10)
	pipeline := newPipeline(cfg, repository, &modeHandler{})
	pipeline.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("body"))))
	exchange := repository.List()[0]
	if string(exchange.Request.Preview) != "body" || exchange.Request.PreviewTruncated {
		t.Fatalf("request = %+v", exchange.Request)
	}
}

func TestPipelineProtectsStoredRequestAndSnapshotFromModeHandler(t *testing.T) {
	cfg := config.Defaults()
	cfg.Capture.Headers = map[string][]string{"X-Snapshot": {"original"}}
	repository := store.NewMemory(10)
	pipeline := newPipeline(cfg, repository, mutatingHandler{})
	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("body")))
	request.Header.Set("X-Test", "original")
	pipeline.ServeHTTP(httptest.NewRecorder(), request)
	exchange := repository.List()[0]
	if string(exchange.Request.Body) != "body" || exchange.Request.Headers.Get("X-Test") != "original" || exchange.Configuration.Capture.Headers["X-Snapshot"][0] != "original" {
		t.Fatalf("stored exchange mutated: %+v", exchange)
	}
}

func TestPipelineRejectsLargeHeadersBeforeReadingBody(t *testing.T) {
	cfg := config.Defaults()
	cfg.Limits.RequestHeaderBytes = 2
	repository := store.NewMemory(10)
	body := &observedReader{Reader: bytes.NewReader([]byte("unread"))}
	recorder := httptest.NewRecorder()
	newPipeline(cfg, repository, &modeHandler{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/", body))
	if recorder.Code != http.StatusRequestHeaderFieldsTooLarge || body.reads.Load() != 0 || repository.List()[0].Error.Category != "request_headers_too_large" {
		t.Fatalf("response=%d reads=%d exchange=%+v", recorder.Code, body.reads.Load(), repository.List()[0])
	}
}

func TestPipelineRecordsBodyReadFailure(t *testing.T) {
	cfg := config.Defaults()
	repository := store.NewMemory(10)
	request := httptest.NewRequest(http.MethodPost, "/", io.NopCloser(errorReader{}))
	recorder := httptest.NewRecorder()
	newPipeline(cfg, repository, &modeHandler{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || repository.List()[0].Error.Category != "request_body_read_failed" {
		t.Fatalf("response=%d exchange=%+v", recorder.Code, repository.List()[0])
	}
}

func TestPipelineHandlesModeFailureAndCancellation(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantState capture.State
	}{
		{name: "failure", err: io.ErrUnexpectedEOF, wantState: capture.StateFailed},
		{name: "cancellation", err: context.Canceled, wantState: capture.StateCancelled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := store.NewMemory(10)
			recorder := httptest.NewRecorder()
			newPipeline(config.Defaults(), repository, &modeHandler{err: test.err}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
			if recorder.Code != http.StatusBadGateway || repository.List()[0].State != test.wantState {
				t.Fatalf("response=%d exchange=%+v", recorder.Code, repository.List()[0])
			}
		})
	}
}

func TestPipelineRecordsTypedProxyFailureAndTiming(t *testing.T) {
	cfg := config.Defaults()
	cfg.Mode = config.ModeProxy
	repository := store.NewMemory(10)
	now := time.Now()
	timing := &capture.ProxyTiming{UpstreamStartedAt: &now}
	decodedSize := int64(3)
	handler := &modeHandler{
		result: capture.ModeResult{
			EffectiveUpstream: "http://upstream.test/path", ProxyTiming: timing,
			RequestInspection:                   &capture.Inspection{Preview: []byte("abc"), DecodedPreviewBytes: &decodedSize},
			UpstreamResponseHeaderBytesEstimate: 55, UpstreamObservedResponseBodyBytes: 9,
		},
		err: &capture.ModeError{Status: http.StatusGatewayTimeout, Category: "upstream_timeout", Message: "Upstream request timed out.", Cause: context.DeadlineExceeded},
	}
	recorder := httptest.NewRecorder()
	newPipeline(cfg, repository, handler).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("raw"))))
	exchange := repository.List()[0]
	if recorder.Code != http.StatusGatewayTimeout || exchange.State != capture.StateFailed || exchange.Error.Category != "upstream_timeout" {
		t.Fatalf("response=%d exchange=%+v", recorder.Code, exchange)
	}
	if exchange.Response == nil || exchange.Response.Origin != "proxy_error" || !exchange.Response.BodyComplete {
		t.Fatalf("proxy error response = %+v", exchange.Response)
	}
	if exchange.EffectiveUpstream == "" || exchange.UpstreamResponseHeaderBytesEstimate != 55 || exchange.UpstreamObservedResponseBodyBytes != 9 || string(exchange.Request.Preview) != "abc" {
		t.Fatalf("proxy metadata = %+v", exchange)
	}
	if exchange.ProxyTiming == nil || exchange.ProxyTiming.DownstreamWriteDurationUS == nil || exchange.ProxyTiming.ProxyTotalDurationUS == nil || exchange.ProxyTiming.DownstreamCompletedAt == nil {
		t.Fatalf("timing = %+v", exchange.ProxyTiming)
	}
}

func TestPipelineProxyPublishesCapturedUpstreamResponse(t *testing.T) {
	cfg := config.Defaults()
	cfg.Mode = config.ModeProxy
	repository := store.NewMemory(10)
	bus := events.NewBus(events.Limits{ReplayCount: 10, ReplayBytes: 1 << 20, SubscriberCount: 2, SubscriberBytes: 1 << 20}, time.Now)
	upstreamDuration := int64(5)
	handler := &modeHandler{result: capture.ModeResult{
		Response:          capture.DownstreamResponse{Status: http.StatusCreated, Headers: http.Header{"X-Test": {"value"}}, Body: []byte("reply")},
		CapturedResponse:  &capture.Response{Status: http.StatusCreated, Origin: "upstream", Body: []byte("reply"), HeaderBytesEstimate: 12},
		EffectiveUpstream: "https://example.test/path",
		ProxyTiming:       &capture.ProxyTiming{UpstreamTotalDurationUS: &upstreamDuration},
	}}
	pipeline := capture.NewPipeline(capture.PipelineOptions{
		Snapshot: func() capture.ConfigSnapshot { return capture.NewConfigSnapshot(cfg, 1) }, Repository: repository, Publisher: bus,
		Handlers: map[string]capture.ModeHandler{config.ModeProxy: handler}, ID: func() (string, error) { return "proxy", nil }, Now: time.Now,
	})
	recorder := httptest.NewRecorder()
	pipeline.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	exchange, _ := repository.Get("proxy")
	if recorder.Code != http.StatusCreated || recorder.Body.String() != "reply" || exchange.State != capture.StateCompleted || exchange.Revision != 5 || exchange.EffectiveUpstream == "" {
		t.Fatalf("response=%d/%q exchange=%+v", recorder.Code, recorder.Body.String(), exchange)
	}
	subscription := bus.Subscribe("")
	defer subscription.Close()
	if len(subscription.Replay) != 3 || subscription.Replay[2].Type != events.ResponseCompleted {
		t.Fatalf("events=%+v", subscription.Replay)
	}
	data := subscription.Replay[2].Data.(map[string]any)
	eventTiming := data["proxy_timing"].(*capture.ProxyTiming)
	if eventTiming.UpstreamTotalDurationUS == nil || eventTiming.DownstreamCompletedAt != nil {
		t.Fatalf("response event timing mutated after publication: %+v", eventTiming)
	}
}

func TestPipelineMissingModeHandler(t *testing.T) {
	cfg := config.Defaults()
	repository := store.NewMemory(10)
	pipeline := capture.NewPipeline(capture.PipelineOptions{
		Snapshot: func() capture.ConfigSnapshot { return capture.NewConfigSnapshot(cfg, 1) }, Repository: repository,
		Publisher: events.NewBus(events.Limits{}, time.Now), Handlers: map[string]capture.ModeHandler{}, ID: capture.NewID, Now: time.Now,
	})
	recorder := httptest.NewRecorder()
	pipeline.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusNotImplemented || repository.List()[0].Error.Category != "mode_handler_unavailable" {
		t.Fatalf("response=%d exchange=%+v", recorder.Code, repository.List()[0])
	}
}

func TestPipelineIDFailureDoesNotCreateExchange(t *testing.T) {
	cfg := config.Defaults()
	repository := store.NewMemory(10)
	pipeline := capture.NewPipeline(capture.PipelineOptions{
		Snapshot: func() capture.ConfigSnapshot { return capture.NewConfigSnapshot(cfg, 1) }, Repository: repository,
		Publisher: events.NewBus(events.Limits{}, time.Now), Handlers: map[string]capture.ModeHandler{}, ID: func() (string, error) { return "", io.ErrUnexpectedEOF }, Now: time.Now,
	})
	recorder := httptest.NewRecorder()
	pipeline.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusInternalServerError || len(repository.List()) != 0 {
		t.Fatalf("response=%d exchanges=%d", recorder.Code, len(repository.List()))
	}
}

func TestPipelineRecordsPartialDownstreamWrite(t *testing.T) {
	cfg := config.Defaults()
	cfg.Capture.Body = "body"
	repository := store.NewMemory(10)
	writer := &failingResponseWriter{header: make(http.Header)}
	newPipeline(cfg, repository, capture.CaptureHandler{}).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/", nil))
	exchange := repository.List()[0]
	if exchange.State != capture.StateFailed || exchange.DownstreamDelivery.BodyBytes != 1 || exchange.Error.Category != "downstream_write_failed" {
		t.Fatalf("exchange=%+v", exchange)
	}
}

func TestPipelineRecordsProxyDownstreamWriteFailure(t *testing.T) {
	cfg := config.Defaults()
	cfg.Mode = config.ModeProxy
	repository := store.NewMemory(10)
	handler := &modeHandler{result: capture.ModeResult{
		Response:         capture.DownstreamResponse{Status: http.StatusOK, Body: []byte("body")},
		CapturedResponse: &capture.Response{Status: http.StatusOK, Origin: "upstream", Body: []byte("body"), BodyComplete: true},
		ProxyTiming:      &capture.ProxyTiming{},
	}}
	writer := &failingResponseWriter{header: make(http.Header)}
	newPipeline(cfg, repository, handler).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/", nil))
	exchange := repository.List()[0]
	if exchange.State != capture.StateFailed || exchange.Error.Category != "downstream_write_failed" || exchange.DownstreamDelivery.BodyBytes != 1 || exchange.DownstreamDelivery.Error == "" {
		t.Fatalf("exchange = %+v", exchange)
	}
	if exchange.ProxyTiming == nil || exchange.ProxyTiming.DownstreamWriteDurationUS == nil || exchange.ProxyTiming.ProxyTotalDurationUS == nil {
		t.Fatalf("timing = %+v", exchange.ProxyTiming)
	}
}

func TestPipelineCaptureResponseStaysOutOfHistoryAndEvents(t *testing.T) {
	cfg := config.Defaults()
	cfg.Capture.Status = http.StatusCreated
	cfg.Capture.Headers = map[string][]string{"X-Test": {"one", "two"}}
	cfg.Capture.Body = "configured response"
	repository := store.NewMemory(10)
	bus := events.NewBus(events.Limits{ReplayCount: 10, ReplayBytes: 1 << 20, SubscriberCount: 2, SubscriberBytes: 1 << 20}, time.Now)
	pipeline := capture.NewPipeline(capture.PipelineOptions{
		Snapshot: func() capture.ConfigSnapshot { return capture.NewConfigSnapshot(cfg, 1) }, Repository: repository, Publisher: bus,
		Handlers: map[string]capture.ModeHandler{config.ModeCapture: capture.CaptureHandler{}},
		ID:       func() (string, error) { return "capture", nil }, Now: time.Now,
	})
	recorder := httptest.NewRecorder()
	pipeline.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("incoming"))))
	exchange, found := repository.Get("capture")
	if !found || exchange.State != capture.StateCompleted || exchange.Response != nil || exchange.EffectiveUpstream != "" {
		t.Fatalf("exchange = %+v, found=%v", exchange, found)
	}
	if recorder.Code != http.StatusCreated || recorder.Body.String() != "configured response" || len(recorder.Header().Values("X-Test")) != 2 {
		t.Fatalf("response=%d headers=%v body=%q", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	subscription := bus.Subscribe("")
	defer subscription.Close()
	if len(subscription.Replay) != 2 || subscription.Replay[0].Type != events.ExchangeStarted || subscription.Replay[1].Type != events.RequestCompleted {
		t.Fatalf("events = %+v", subscription.Replay)
	}
}

func TestPipelinePublishesEviction(t *testing.T) {
	cfg := config.Defaults()
	repository := store.NewMemory(1)
	bus := events.NewBus(events.Limits{ReplayCount: 10, ReplayBytes: 1 << 20, SubscriberCount: 2, SubscriberBytes: 1 << 20}, time.Now)
	nextID := 0
	pipeline := capture.NewPipeline(capture.PipelineOptions{
		Snapshot: func() capture.ConfigSnapshot { return capture.NewConfigSnapshot(cfg, 1) }, Repository: repository, Publisher: bus,
		Handlers: map[string]capture.ModeHandler{config.ModeCapture: &modeHandler{}},
		ID:       func() (string, error) { nextID++; return string(rune('0' + nextID)), nil }, Now: time.Now,
	})
	pipeline.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/one", nil))
	pipeline.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/two", nil))
	subscription := bus.Subscribe("")
	defer subscription.Close()
	found := false
	for _, event := range subscription.Replay {
		if event.Type == events.ExchangeEvicted && event.ExchangeID == "1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("eviction event missing: %+v", subscription.Replay)
	}
}

func TestPipelineCapturesRequestAndPublishesLifecycle(t *testing.T) {
	cfg := config.Defaults()
	cfg.Storage.Mode = config.StorageMemory
	repository := store.NewMemory(10)
	bus := events.NewBus(events.Limits{ReplayCount: 10, ReplayBytes: 1 << 20, SubscriberCount: 4, SubscriberBytes: 1 << 20}, time.Now)
	handler := &modeHandler{}
	pipeline := capture.NewPipeline(capture.PipelineOptions{
		Snapshot:   func() capture.ConfigSnapshot { return capture.NewConfigSnapshot(cfg, 9) },
		Repository: repository,
		Publisher:  bus,
		Handlers:   map[string]capture.ModeHandler{config.ModeCapture: handler},
		ID:         func() (string, error) { return "exchange-1", nil },
		Now:        func() time.Time { return time.Unix(100, 0).UTC() },
	})
	request := httptest.NewRequest(http.MethodPost, "http://listener.test/path?q=1", bytes.NewReader([]byte{0, 1, 255}))
	request.Header.Add("X-Test", "one")
	request.Header.Add("X-Test", "two")
	recorder := httptest.NewRecorder()
	pipeline.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent || handler.calls.Load() != 1 {
		t.Fatalf("response=%d calls=%d", recorder.Code, handler.calls.Load())
	}
	exchange, ok := repository.Get("exchange-1")
	if !ok || exchange.State != capture.StateCompleted || exchange.ConfigRevision != 9 || !bytes.Equal(exchange.Request.Body, []byte{0, 1, 255}) {
		t.Fatalf("stored exchange = %+v, found=%v", exchange, ok)
	}
	if len(exchange.Request.Headers["X-Test"]) != 2 || exchange.Request.RawQuery != "q=1" || exchange.Revision != 3 {
		t.Fatalf("request metadata = %+v revision=%d", exchange.Request, exchange.Revision)
	}
	subscription := bus.Subscribe("")
	defer subscription.Close()
	if len(subscription.Replay) != 2 || subscription.Replay[0].Type != events.ExchangeStarted || subscription.Replay[1].Type != events.RequestCompleted {
		t.Fatalf("events = %+v", subscription.Replay)
	}
}

func TestPipelineRejectsOversizedBodyBeforeModeHandler(t *testing.T) {
	cfg := config.Defaults()
	cfg.Limits.RequestBodyBytes = 2
	handler := &modeHandler{}
	repository := store.NewMemory(10)
	pipeline := newPipeline(cfg, repository, handler)
	recorder := httptest.NewRecorder()
	pipeline.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("abc"))))
	if recorder.Code != http.StatusRequestEntityTooLarge || handler.calls.Load() != 0 {
		t.Fatalf("response=%d calls=%d", recorder.Code, handler.calls.Load())
	}
	exchange := repository.List()[0]
	if exchange.State != capture.StateFailed || exchange.Request.BodyComplete || exchange.Request.ObservedBodyBytes != 3 {
		t.Fatalf("exchange = %+v", exchange)
	}
}

func TestPipelineRejectsAtCapacityBeforeReadingBody(t *testing.T) {
	cfg := config.Defaults()
	cfg.Limits.ConcurrentExchanges = 1
	handler := &modeHandler{entered: make(chan struct{}), release: make(chan struct{})}
	pipeline := newPipeline(cfg, store.NewMemory(10), handler)
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		pipeline.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/one", bytes.NewReader(nil)))
	}()
	<-handler.entered
	body := &observedReader{Reader: bytes.NewReader([]byte("unread"))}
	recorder := httptest.NewRecorder()
	pipeline.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/two", body))
	if recorder.Code != http.StatusServiceUnavailable || body.reads.Load() != 0 {
		t.Fatalf("response=%d body reads=%d", recorder.Code, body.reads.Load())
	}
	close(handler.release)
	<-firstDone
}

func TestPipelineReservesPersistenceBeforeBodyAndCommitsTerminalExchange(t *testing.T) {
	cfg := config.Defaults()
	cfg.Mode = config.ModeProxy
	cfg.Storage.Mode = config.StorageSQLite
	repository := &reservationRepository{Memory: store.NewMemory(10), accept: true, committed: make(chan capture.Exchange, 1)}
	handler := &modeHandler{}
	pipeline := newPipeline(cfg, repository, handler)
	recorder := httptest.NewRecorder()
	pipeline.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/persist", bytes.NewReader([]byte("body"))))
	wantReservation := cfg.Limits.RequestBodyBytes + cfg.Limits.ResponseBodyBytes + int64(cfg.Limits.RequestHeaderBytes+cfg.Limits.ResponseHeaderBytes)
	if repository.reserved != wantReservation {
		t.Fatalf("reserved=%d want=%d", repository.reserved, wantReservation)
	}
	select {
	case exchange := <-repository.committed:
		if exchange.State != capture.StateCompleted || string(exchange.Request.Body) != "body" {
			t.Fatalf("committed exchange=%+v", exchange)
		}
	default:
		t.Fatal("terminal exchange was not committed")
	}
}

func TestPipelineRejectsPersistenceCapacityBeforeReadingBody(t *testing.T) {
	cfg := config.Defaults()
	repository := &reservationRepository{Memory: store.NewMemory(10), accept: false, committed: make(chan capture.Exchange, 1)}
	handler := &modeHandler{}
	body := &observedReader{Reader: bytes.NewReader([]byte("unread"))}
	recorder := httptest.NewRecorder()
	pipeline := newPipeline(cfg, repository, handler)
	pipeline.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/", body))
	if recorder.Code != http.StatusServiceUnavailable || body.reads.Load() != 0 || handler.calls.Load() != 0 || len(repository.List()) != 0 {
		t.Fatalf("status=%d reads=%d calls=%d exchanges=%d", recorder.Code, body.reads.Load(), handler.calls.Load(), len(repository.List()))
	}
	if pipeline.Metrics().AdmissionRejected != 1 {
		t.Fatalf("admission metrics = %+v", pipeline.Metrics())
	}
}

func TestPipelinePersistenceReservationSaturatesWithoutOverflow(t *testing.T) {
	cfg := config.Defaults()
	cfg.Mode = config.ModeProxy
	cfg.Limits.RequestBodyBytes = int64(^uint64(0) >> 1)
	repository := &reservationRepository{Memory: store.NewMemory(10), accept: false, committed: make(chan capture.Exchange, 1)}
	newPipeline(cfg, repository, &modeHandler{}).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if repository.reserved != int64(^uint64(0)>>1) {
		t.Fatalf("reserved=%d", repository.reserved)
	}
}

func TestPipelineUsesCurrentConcurrencyLimitAtAdmission(t *testing.T) {
	cfg := config.Defaults()
	cfg.Limits.ConcurrentExchanges = 1
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	repository := store.NewMemory(10)
	pipeline := capture.NewPipeline(capture.PipelineOptions{
		Snapshot: func() capture.ConfigSnapshot { return capture.NewConfigSnapshot(cfg, 1) }, Repository: repository,
		Publisher: events.NewBus(events.Limits{}, time.Now), Handlers: map[string]capture.ModeHandler{config.ModeCapture: waitingHandler{entered: entered, release: release}}, ID: capture.NewID, Now: time.Now,
	})
	done := make(chan struct{}, 2)
	go func() {
		pipeline.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/one", nil))
		done <- struct{}{}
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first request did not enter handler")
	}
	cfg.Limits.ConcurrentExchanges = 2
	go func() {
		pipeline.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/two", nil))
		done <- struct{}{}
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("updated concurrency limit was not applied")
	}
	close(release)
	<-done
	<-done
}

func TestPipelineReportsLiveAdmissionAndBodyLimitMetrics(t *testing.T) {
	cfg := config.Defaults()
	cfg.Limits.ConcurrentExchanges = 1
	cfg.Limits.RequestBodyBytes = 1
	entered := make(chan struct{})
	release := make(chan struct{})
	pipeline := capture.NewPipeline(capture.PipelineOptions{
		Snapshot:   func() capture.ConfigSnapshot { return capture.NewConfigSnapshot(cfg, 1) },
		Repository: store.NewMemory(10),
		Publisher:  events.NewBus(events.Limits{}, time.Now),
		Handlers:   map[string]capture.ModeHandler{cfg.Mode: &modeHandler{entered: entered, release: release}},
		ID:         capture.NewID,
		Now:        time.Now,
	})
	done := make(chan struct{})
	go func() {
		pipeline.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/active", nil))
		close(done)
	}()
	<-entered
	if metrics := pipeline.Metrics(); metrics.CurrentRequests != 1 {
		t.Fatalf("current requests = %d, want 1", metrics.CurrentRequests)
	}
	rejected := httptest.NewRecorder()
	pipeline.ServeHTTP(rejected, httptest.NewRequest(http.MethodPost, "/rejected", strings.NewReader("xx")))
	if rejected.Code != http.StatusServiceUnavailable || pipeline.Metrics().AdmissionRejected != 1 {
		t.Fatalf("admission response = %d, metrics = %+v", rejected.Code, pipeline.Metrics())
	}
	close(release)
	<-done

	oversized := httptest.NewRecorder()
	pipeline.ServeHTTP(oversized, httptest.NewRequest(http.MethodPost, "/large", strings.NewReader("xx")))
	metrics := pipeline.Metrics()
	if oversized.Code != http.StatusRequestEntityTooLarge || metrics.CurrentRequests != 0 || metrics.BodyTooLarge != 1 {
		t.Fatalf("oversized response = %d, metrics = %+v", oversized.Code, metrics)
	}
}

type observedReader struct {
	*bytes.Reader
	reads atomic.Int32
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

type failingResponseWriter struct {
	header http.Header
	status int
}

func (writer *failingResponseWriter) Header() http.Header { return writer.header }

func (writer *failingResponseWriter) WriteHeader(status int) { writer.status = status }

func (*failingResponseWriter) Write([]byte) (int, error) { return 1, io.ErrClosedPipe }

func (reader *observedReader) Read(data []byte) (int, error) {
	reader.reads.Add(1)
	return reader.Reader.Read(data)
}

func newPipeline(cfg config.Config, repository capture.Repository, handler capture.ModeHandler) *capture.Pipeline {
	return capture.NewPipeline(capture.PipelineOptions{
		Snapshot:   func() capture.ConfigSnapshot { return capture.NewConfigSnapshot(cfg, 1) },
		Repository: repository,
		Publisher:  events.NewBus(events.Limits{ReplayCount: 10, ReplayBytes: 1 << 20, SubscriberCount: 2, SubscriberBytes: 1 << 20}, time.Now),
		Handlers:   map[string]capture.ModeHandler{cfg.Mode: handler},
		ID:         capture.NewID,
		Now:        time.Now,
	})
}

var _ io.Reader = (*observedReader)(nil)
